// A roster pins network consent chains only in its v2 schema; a roster without
// them keeps the v1 schema and its exact canonical bytes. Identities and keys
// are synthetic.
package payoutartifact

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/protocol"
)

// A minimal valid roster: two owners in two networks, each an expected
// provider without a provider wallet head.
func networkWalletTestAuthority() WholeWorkAuthority {
	domain := protocol.ClientKeyHistoryDomain{ChainID: 964, GenesisHash: [32]byte{1}, Netuid: 25, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 6}
	authority := WholeWorkAuthority{
		Domain:           domain,
		Epoch:            7,
		Start:            Boundary{Number: 100, Hash: "0x" + strings.Repeat("aa", 32)},
		End:              Boundary{Number: 200, Hash: "0x" + strings.Repeat("bb", 32)},
		RequestPublicKey: [32]byte{9},
		PriorContracts:   []WholeWorkPriorContract{},
	}
	for index := 0; index < 2; index++ {
		owner := WholeWorkOwner{ClientId: [16]byte{byte(index + 1)}, NetworkId: [16]byte{byte((index + 1) * 10)}, Generation: [16]byte{byte(index + 31)}, PublicKey: [32]byte{byte(index + 41)}}
		authority.Owners = append(authority.Owners, owner)
		authority.ExpectedProviders = append(authority.ExpectedProviders, WholeWorkExpectedProvider{ClientId: owner.ClientId, NetworkId: owner.NetworkId})
	}
	return authority
}

func TestWholeWorkAuthorityWithoutNetworkWalletsKeepsV1Bytes(t *testing.T) {
	key, err := crypto.HexToECDSA(strings.Repeat("39", 32))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignWholeWorkAuthority(t.Context(), networkWalletTestAuthority(), key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if signed.Schema != WholeWorkAuthoritySchema || bytes.Contains(raw, []byte("network_wallets")) {
		t.Fatalf("a roster without network chains changed form: %s", signed.Schema)
	}
	decoded, err := DecodeWholeWorkAuthority(t.Context(), raw, crypto.PubkeyToAddress(key.PublicKey))
	if err != nil || decoded.Schema != WholeWorkAuthoritySchema {
		t.Fatal("v1 roster did not round trip", err)
	}
}

func TestWholeWorkAuthorityNetworkWalletsSignAsV2(t *testing.T) {
	key, err := crypto.HexToECDSA(strings.Repeat("39", 32))
	if err != nil {
		t.Fatal(err)
	}
	authority := networkWalletTestAuthority()
	authority.NetworkWallets = []WholeWorkNetworkWallet{{NetworkId: [16]byte{20}, WalletHeadHash: strings.Repeat("cd", 32), WalletGeneration: 2}}
	signed, err := SignWholeWorkAuthority(t.Context(), authority, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeWholeWorkAuthority(t.Context(), raw, crypto.PubkeyToAddress(key.PublicKey))
	if err != nil || decoded.Schema != WholeWorkAuthorityNetworkWalletSchema {
		t.Fatal("v2 roster did not round trip", err)
	}
	if wallet, found := decoded.NetworkWallet([16]byte{20}); !found || wallet.WalletGeneration != 2 {
		t.Fatal("pinned network chain not found", wallet, found)
	}
	if _, found := decoded.NetworkWallet([16]byte{10}); found {
		t.Fatal("an unpinned network gained a chain")
	}
	// the schema is part of the signed bytes: a v2 roster cannot claim v1
	downgraded := bytes.Replace(raw, []byte(WholeWorkAuthorityNetworkWalletSchema), []byte(WholeWorkAuthoritySchema), 1)
	if _, err := DecodeWholeWorkAuthority(t.Context(), downgraded, crypto.PubkeyToAddress(key.PublicKey)); err == nil {
		t.Fatal("a v2 roster was admitted as v1")
	}
}

func TestWholeWorkAuthorityRefusesInvalidNetworkWallets(t *testing.T) {
	key, err := crypto.HexToECDSA(strings.Repeat("39", 32))
	if err != nil {
		t.Fatal(err)
	}
	head := strings.Repeat("cd", 32)
	cases := [][]WholeWorkNetworkWallet{
		// out of order and duplicated
		{{NetworkId: [16]byte{20}, WalletHeadHash: head, WalletGeneration: 1}, {NetworkId: [16]byte{10}, WalletHeadHash: head, WalletGeneration: 1}},
		{{NetworkId: [16]byte{10}, WalletHeadHash: head, WalletGeneration: 1}, {NetworkId: [16]byte{10}, WalletHeadHash: head, WalletGeneration: 1}},
		// a network without an expected provider, and the zero network
		{{NetworkId: [16]byte{30}, WalletHeadHash: head, WalletGeneration: 1}},
		{{NetworkId: [16]byte{}, WalletHeadHash: head, WalletGeneration: 1}},
		// malformed heads and generations
		{{NetworkId: [16]byte{10}, WalletHeadHash: "", WalletGeneration: 1}},
		{{NetworkId: [16]byte{10}, WalletHeadHash: strings.ToUpper(head), WalletGeneration: 1}},
		{{NetworkId: [16]byte{10}, WalletHeadHash: head, WalletGeneration: 0}},
		{{NetworkId: [16]byte{10}, WalletHeadHash: head, WalletGeneration: protocol.MaxWalletMappingHistory + 1}},
	}
	for index, wallets := range cases {
		authority := networkWalletTestAuthority()
		authority.NetworkWallets = wallets
		if _, err := SignWholeWorkAuthority(t.Context(), authority, key); err == nil {
			t.Errorf("invalid network chains %d were signed", index)
		}
	}
	// v2 without any network chain has no canonical form
	signed, err := SignWholeWorkAuthority(t.Context(), networkWalletTestAuthority(), key)
	if err != nil {
		t.Fatal(err)
	}
	signed.Schema = WholeWorkAuthorityNetworkWalletSchema
	if err := signed.Verify(t.Context(), crypto.PubkeyToAddress(key.PublicKey)); err == nil {
		t.Fatal("a v2 roster without network chains verified")
	}
}
