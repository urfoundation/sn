// The independent verifier resolves each expected provider's earning wallet
// from retained provider and network consent originals, with the same
// precedence as settlement. Keys and identities are synthetic.
package main

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The roster epoch, its window start and the operator boundary consents are
// issued at.
const economicNetworkWalletTestEpoch = 42
const economicNetworkWalletTestStartBlock = 1000
const economicNetworkWalletTestStartUnix = 5000

var economicNetworkWalletTestBoundary = protocol.ClientKeyEffectiveBoundary{Epoch: 41, Block: 999, Hash: [32]byte{12}}

var economicNetworkWalletTestDomain = protocol.ClientKeyHistoryDomain{ChainID: 964, GenesisHash: [32]byte{1}, Netuid: 25, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 6}

// Signs with a synthetic coldkey seed; returns the original and the coldkey.
func economicNetworkWalletTestSign(t *testing.T, seed byte, message func(coldkey [32]byte) string) (protocol.WalletMappingConsent, [32]byte) {
	t.Helper()
	key, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	coldkey := key.Public().Encode()
	text := message(coldkey)
	signature, err := key.ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(text)))
	if err != nil {
		t.Fatal(err)
	}
	return protocol.WalletMappingConsent{Message: text, Signature: signature.Encode()}, coldkey
}

// A prospective provider consent for clientId effective from fromEpoch.
func economicNetworkWalletTestProvider(t *testing.T, clientId [16]byte, networkId [16]byte, seed byte, fromEpoch uint64) (protocol.WalletMappingConsent, [32]byte, string) {
	t.Helper()
	operator, err := crypto.HexToECDSA(strings.Repeat("37", 32))
	if err != nil {
		t.Fatal(err)
	}
	original, coldkey := economicNetworkWalletTestSign(t, seed, func(coldkey [32]byte) string {
		statement := protocol.WalletMappingStatement{Domain: economicNetworkWalletTestDomain, UserId: [16]byte{7}, ClientId: clientId, NetworkId: networkId, Coldkey: coldkey, Generation: 1, Nonce: [32]byte{seed}, IssuedAt: 4000, ExpiresAt: 4300, FromEpoch: fromEpoch, ThroughEpoch: fromEpoch + 100}
		if err := protocol.SignProspectiveWalletMapping(&statement, economicNetworkWalletTestBoundary, operator); err != nil {
			t.Fatal(err)
		}
		message, err := statement.Message()
		if err != nil {
			t.Fatal(err)
		}
		return message
	})
	_, hash, err := protocol.VerifyWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, coldkey, hex.EncodeToString(hash[:])
}

// A prospective network consent for networkId effective from fromEpoch.
func economicNetworkWalletTestNetwork(t *testing.T, networkId [16]byte, seed byte, fromEpoch uint64) (protocol.WalletMappingConsent, [32]byte, string) {
	t.Helper()
	operator, err := crypto.HexToECDSA(strings.Repeat("37", 32))
	if err != nil {
		t.Fatal(err)
	}
	original, coldkey := economicNetworkWalletTestSign(t, seed, func(coldkey [32]byte) string {
		statement := protocol.NetworkWalletMappingStatement{Domain: economicNetworkWalletTestDomain, UserId: [16]byte{7}, NetworkId: networkId, Coldkey: coldkey, Generation: 1, Nonce: [32]byte{seed}, IssuedAt: 4000, ExpiresAt: 4300, FromEpoch: fromEpoch, ThroughEpoch: fromEpoch + 100}
		if err := protocol.SignProspectiveNetworkWalletMapping(&statement, economicNetworkWalletTestBoundary, operator); err != nil {
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
	return original, coldkey, hex.EncodeToString(hash[:])
}

// The operator signer the synthetic consents carry.
func economicNetworkWalletTestSigner(t *testing.T) common.Address {
	t.Helper()
	operator, err := crypto.HexToECDSA(strings.Repeat("37", 32))
	if err != nil {
		t.Fatal(err)
	}
	return crypto.PubkeyToAddress(operator.PublicKey)
}

// Three providers in network 9: one with its own effective consent, one
// without a provider chain, and one whose own consent starts after the epoch.
// The network consent pays the last two.
func TestEconomicProviderEarningWalletsApplyPrecedence(t *testing.T) {
	networkId := [16]byte{9}
	owned, ownedColdkey, ownedHead := economicNetworkWalletTestProvider(t, [16]byte{1}, networkId, 21, economicNetworkWalletTestEpoch)
	later, _, laterHead := economicNetworkWalletTestProvider(t, [16]byte{3}, networkId, 23, economicNetworkWalletTestEpoch+1)
	network, networkColdkey, networkHead := economicNetworkWalletTestNetwork(t, networkId, 29, economicNetworkWalletTestEpoch)
	authority := &payoutartifact.WholeWorkAuthority{
		Domain: economicNetworkWalletTestDomain,
		Epoch:  economicNetworkWalletTestEpoch,
		ExpectedProviders: []payoutartifact.WholeWorkExpectedProvider{
			{ClientId: [16]byte{1}, NetworkId: networkId, WalletHeadHash: ownedHead, WalletGeneration: 1},
			{ClientId: [16]byte{2}, NetworkId: networkId},
			{ClientId: [16]byte{3}, NetworkId: networkId, WalletHeadHash: laterHead, WalletGeneration: 1},
		},
		NetworkWallets: []payoutartifact.WholeWorkNetworkWallet{{NetworkId: networkId, WalletHeadHash: networkHead, WalletGeneration: 1}},
	}
	originals := &economicProviderOriginals{
		Wallets: []economicProviderWalletOriginal{
			{ClientId: [16]byte{1}, Originals: []protocol.WalletMappingConsent{owned}},
			{ClientId: [16]byte{3}, Originals: []protocol.WalletMappingConsent{later}},
		},
		NetworkWallets: []economicNetworkWalletOriginal{{NetworkId: networkId, Originals: []protocol.WalletMappingConsent{network}}},
	}
	wallets, err := economicProviderEarningWallets(t.Context(), authority, originals, economicNetworkWalletTestSigner(t), economicNetworkWalletTestStartBlock, economicNetworkWalletTestStartUnix)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[[16]byte]struct {
		mode    string
		coldkey [32]byte
	}{
		{1}: {mode: protocol.EarningWalletModeProvider, coldkey: ownedColdkey},
		{2}: {mode: protocol.EarningWalletModeNetwork, coldkey: networkColdkey},
		{3}: {mode: protocol.EarningWalletModeNetwork, coldkey: networkColdkey},
	}
	for clientId, want := range expected {
		wallet := wallets[clientId]
		if wallet == nil || wallet.Mode != want.mode || wallet.Coldkey != want.coldkey || wallet.ClientId != clientId || wallet.NetworkId != networkId {
			t.Errorf("client %x earns to %+v, want %s", clientId[:1], wallet, want.mode)
		}
	}
}

// Missing or contradictory provider evidence never falls back, network
// evidence must exist for a fallback, and unconsulted network evidence is
// refused.
func TestEconomicProviderEarningWalletsRefuseUnsafeFallbacks(t *testing.T) {
	networkId := [16]byte{9}
	owned, _, ownedHead := economicNetworkWalletTestProvider(t, [16]byte{1}, networkId, 21, economicNetworkWalletTestEpoch)
	network, _, networkHead := economicNetworkWalletTestNetwork(t, networkId, 29, economicNetworkWalletTestEpoch)
	signer := economicNetworkWalletTestSigner(t)
	roster := func(providers ...payoutartifact.WholeWorkExpectedProvider) *payoutartifact.WholeWorkAuthority {
		return &payoutartifact.WholeWorkAuthority{Domain: economicNetworkWalletTestDomain, Epoch: economicNetworkWalletTestEpoch, ExpectedProviders: providers, NetworkWallets: []payoutartifact.WholeWorkNetworkWallet{{NetworkId: networkId, WalletHeadHash: networkHead, WalletGeneration: 1}}}
	}
	withHead := payoutartifact.WholeWorkExpectedProvider{ClientId: [16]byte{1}, NetworkId: networkId, WalletHeadHash: ownedHead, WalletGeneration: 1}
	withoutHead := payoutartifact.WholeWorkExpectedProvider{ClientId: [16]byte{2}, NetworkId: networkId}
	networkOriginals := []economicNetworkWalletOriginal{{NetworkId: networkId, Originals: []protocol.WalletMappingConsent{network}}}
	cases := []struct {
		name      string
		authority *payoutartifact.WholeWorkAuthority
		originals *economicProviderOriginals
		want      error
	}{
		{
			name:      "missing provider originals",
			authority: roster(withHead),
			originals: &economicProviderOriginals{Wallets: []economicProviderWalletOriginal{{ClientId: [16]byte{1}, Originals: nil}}, NetworkWallets: networkOriginals},
			want:      protocol.ErrWalletMappingUnavailable,
		},
		{
			name:      "provider originals of another client",
			authority: roster(withHead),
			originals: &economicProviderOriginals{Wallets: []economicProviderWalletOriginal{{ClientId: [16]byte{2}, Originals: []protocol.WalletMappingConsent{owned}}}, NetworkWallets: networkOriginals},
			want:      protocol.ErrWalletMappingIntegrity,
		},
		{
			name:      "fallback without network originals",
			authority: roster(withoutHead),
			originals: &economicProviderOriginals{},
			want:      protocol.ErrWalletMappingUnavailable,
		},
		{
			name:      "unconsulted network originals",
			authority: roster(withHead),
			originals: &economicProviderOriginals{Wallets: []economicProviderWalletOriginal{{ClientId: [16]byte{1}, Originals: []protocol.WalletMappingConsent{owned}}}, NetworkWallets: networkOriginals},
			want:      protocol.ErrWalletMappingIntegrity,
		},
	}
	for _, c := range cases {
		wallets, err := economicProviderEarningWallets(t.Context(), c.authority, c.originals, signer, economicNetworkWalletTestStartBlock, economicNetworkWalletTestStartUnix)
		if wallets != nil || !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, %v", c.name, wallets, err)
		}
	}
	// a fallback provider in a network without a pinned chain is unavailable
	authority := roster(withoutHead)
	authority.NetworkWallets = nil
	if wallets, err := economicProviderEarningWallets(t.Context(), authority, &economicProviderOriginals{}, signer, economicNetworkWalletTestStartBlock, economicNetworkWalletTestStartUnix); wallets != nil || !errors.Is(err, protocol.ErrWalletMappingUnavailable) {
		t.Fatal("a provider without any consent gained a wallet", err)
	}
}
