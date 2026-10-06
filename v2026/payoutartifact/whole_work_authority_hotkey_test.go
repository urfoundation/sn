// A roster pins hotkey delegation chains only in its v3 schema; a roster
// without them keeps the v2 or v1 schema and its exact canonical bytes.
// Identities and keys are synthetic.
package payoutartifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The SHA-256 and length of the exact signed bytes of the v1 and v2 fixture
// rosters, captured before v3 existed. secp256k1 signing is deterministic
// (RFC 6979), so any change to these bytes changes the digest.
const wholeWorkAuthorityV1Sha256 = "aa54fd6416e083c2ddc481f751c7aaa7fc30ca5565848c4e2e14d73ba028aff4"
const wholeWorkAuthorityV1Bytes = 1862
const wholeWorkAuthorityV2Sha256 = "0c1a7a0ef76c17a45743c19dfe9e6586cac781a0d55c0a9dcbd9f4b0940873da"
const wholeWorkAuthorityV2Bytes = 2037

// Signs fixture rosters with one synthetic key, and returns that key's address.
func hotkeyDelegationTestSigner(t *testing.T) (common.Address, func(WholeWorkAuthority) (WholeWorkAuthority, []byte, error)) {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("39", 32))
	if err != nil {
		t.Fatal(err)
	}
	return crypto.PubkeyToAddress(key.PublicKey), func(authority WholeWorkAuthority) (WholeWorkAuthority, []byte, error) {
		signed, err := SignWholeWorkAuthority(t.Context(), authority, key)
		if err != nil {
			return signed, nil, err
		}
		raw, err := signed.Bytes(t.Context())
		return signed, raw, err
	}
}

func TestWholeWorkAuthorityV1AndV2KeepExactSignedBytes(t *testing.T) {
	_, sign := hotkeyDelegationTestSigner(t)
	v2 := networkWalletTestAuthority()
	v2.NetworkWallets = []WholeWorkNetworkWallet{{NetworkId: [16]byte{20}, WalletHeadHash: strings.Repeat("cd", 32), WalletGeneration: 2}}
	for _, c := range []struct {
		authority WholeWorkAuthority
		schema    string
		digest    string
		length    int
	}{
		{authority: networkWalletTestAuthority(), schema: WholeWorkAuthoritySchema, digest: wholeWorkAuthorityV1Sha256, length: wholeWorkAuthorityV1Bytes},
		{authority: v2, schema: WholeWorkAuthorityNetworkWalletSchema, digest: wholeWorkAuthorityV2Sha256, length: wholeWorkAuthorityV2Bytes},
	} {
		signed, raw, err := sign(c.authority)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		if signed.Schema != c.schema || hex.EncodeToString(digest[:]) != c.digest || len(raw) != c.length || bytes.Contains(raw, []byte("hotkey_delegations")) {
			t.Errorf("%s roster bytes changed: %x (%d bytes)", c.schema, digest, len(raw))
		}
	}
}

func TestWholeWorkAuthorityHotkeyDelegationsSignAsV3(t *testing.T) {
	signer, sign := hotkeyDelegationTestSigner(t)
	authority := networkWalletTestAuthority()
	authority.HotkeyDelegations = []WholeWorkHotkeyDelegation{{NetworkId: [16]byte{10}, DelegationHeadHash: strings.Repeat("ef", 32), DelegationGeneration: 3}}
	signed, raw, err := sign(authority)
	if err != nil {
		t.Fatal(err)
	}
	if signed.Schema != WholeWorkAuthorityHotkeyDelegationSchema || bytes.Contains(raw, []byte("network_wallets")) {
		t.Fatal("a roster with a delegation did not sign as v3 alone", signed.Schema)
	}
	decoded, err := DecodeWholeWorkAuthority(t.Context(), raw, signer)
	if err != nil || decoded.Schema != WholeWorkAuthorityHotkeyDelegationSchema {
		t.Fatal("v3 roster did not round trip", err)
	}
	if delegation, found := decoded.HotkeyDelegation([16]byte{10}); !found || delegation.DelegationGeneration != 3 || delegation.DelegationHeadHash != strings.Repeat("ef", 32) {
		t.Fatal("pinned delegation chain not found", delegation, found)
	}
	if _, found := decoded.HotkeyDelegation([16]byte{20}); found {
		t.Fatal("an unpinned network gained a delegation")
	}
	// network chains and delegation chains may pin the same network
	authority.NetworkWallets = []WholeWorkNetworkWallet{{NetworkId: [16]byte{10}, WalletHeadHash: strings.Repeat("cd", 32), WalletGeneration: 1}}
	signed, raw, err = sign(authority)
	if err != nil || signed.Schema != WholeWorkAuthorityHotkeyDelegationSchema {
		t.Fatal("a roster with both chain kinds did not sign as v3", err)
	}
	decoded, err = DecodeWholeWorkAuthority(t.Context(), raw, signer)
	if err != nil || len(decoded.NetworkWallets) != 1 || len(decoded.HotkeyDelegations) != 1 {
		t.Fatal("a v3 roster with both chain kinds did not round trip", err)
	}
	// the schema is part of the signed bytes: a v3 roster cannot claim v2 or v1
	for _, schema := range []string{WholeWorkAuthorityNetworkWalletSchema, WholeWorkAuthoritySchema} {
		downgraded := bytes.Replace(raw, []byte(WholeWorkAuthorityHotkeyDelegationSchema), []byte(schema), 1)
		if _, err := DecodeWholeWorkAuthority(t.Context(), downgraded, signer); err == nil {
			t.Errorf("a v3 roster was admitted as %s", schema)
		}
	}
}

// A reader built before v3 decodes the roster without hotkey_delegations,
// because unknown JSON is ignored. That view verifies under no schema and
// does not re-encode to the signed bytes, so an old reader fails closed.
func TestWholeWorkAuthorityV3RefusedByOldReaders(t *testing.T) {
	signer, sign := hotkeyDelegationTestSigner(t)
	for _, networkWallets := range [][]WholeWorkNetworkWallet{nil, {{NetworkId: [16]byte{20}, WalletHeadHash: strings.Repeat("cd", 32), WalletGeneration: 1}}} {
		authority := networkWalletTestAuthority()
		authority.NetworkWallets = networkWallets
		authority.HotkeyDelegations = []WholeWorkHotkeyDelegation{{NetworkId: [16]byte{10}, DelegationHeadHash: strings.Repeat("ef", 32), DelegationGeneration: 1}}
		_, raw, err := sign(authority)
		if err != nil {
			t.Fatal(err)
		}
		var old WholeWorkAuthority
		if err := json.Unmarshal(raw, &old); err != nil {
			t.Fatal(err)
		}
		old.HotkeyDelegations = nil
		for _, schema := range []string{WholeWorkAuthorityHotkeyDelegationSchema, WholeWorkAuthorityNetworkWalletSchema, WholeWorkAuthoritySchema} {
			old.Schema = schema
			if err := old.Verify(t.Context(), signer); err == nil {
				t.Errorf("an old reader's view of a v3 roster verified as %s", schema)
			}
		}
		old.Schema = WholeWorkAuthorityHotkeyDelegationSchema
		if reencoded, err := json.Marshal(old); err != nil || bytes.Equal(reencoded, raw) {
			t.Error("an old reader's view kept the canonical v3 bytes", err)
		}
	}
}

// A provider without its own head is permitted under a delegation of its
// network, beside a provider with its own head.
func TestWholeWorkAuthorityV3AllowsEmptyProviderHeadUnderDelegation(t *testing.T) {
	signer, sign := hotkeyDelegationTestSigner(t)
	authority := networkWalletTestAuthority()
	authority.ExpectedProviders[1].WalletHeadHash, authority.ExpectedProviders[1].WalletGeneration = strings.Repeat("ab", 32), 1
	authority.HotkeyDelegations = []WholeWorkHotkeyDelegation{
		{NetworkId: [16]byte{10}, DelegationHeadHash: strings.Repeat("ef", 32), DelegationGeneration: 1},
		{NetworkId: [16]byte{20}, DelegationHeadHash: strings.Repeat("ee", 32), DelegationGeneration: 2},
	}
	_, raw, err := sign(authority)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeWholeWorkAuthority(t.Context(), raw, signer)
	if err != nil || decoded.ExpectedProviders[0].WalletHeadHash != "" || decoded.ExpectedProviders[0].WalletGeneration != 0 || len(decoded.HotkeyDelegations) != 2 {
		t.Fatal("a delegated provider without its own head was refused", err)
	}
	// an empty head with a generation stays malformed
	authority.ExpectedProviders[0].WalletGeneration = 1
	if _, _, err := sign(authority); err == nil {
		t.Fatal("an empty provider head with a generation was signed")
	}
}

func TestWholeWorkAuthorityRefusesInvalidHotkeyDelegations(t *testing.T) {
	_, sign := hotkeyDelegationTestSigner(t)
	head := strings.Repeat("ef", 32)
	cases := [][]WholeWorkHotkeyDelegation{
		// out of order and duplicated
		{{NetworkId: [16]byte{20}, DelegationHeadHash: head, DelegationGeneration: 1}, {NetworkId: [16]byte{10}, DelegationHeadHash: head, DelegationGeneration: 1}},
		{{NetworkId: [16]byte{10}, DelegationHeadHash: head, DelegationGeneration: 1}, {NetworkId: [16]byte{10}, DelegationHeadHash: head, DelegationGeneration: 1}},
		// a network without an expected provider, and the zero network
		{{NetworkId: [16]byte{30}, DelegationHeadHash: head, DelegationGeneration: 1}},
		{{NetworkId: [16]byte{}, DelegationHeadHash: head, DelegationGeneration: 1}},
		// malformed heads and generations
		{{NetworkId: [16]byte{10}, DelegationHeadHash: "", DelegationGeneration: 1}},
		{{NetworkId: [16]byte{10}, DelegationHeadHash: strings.ToUpper(head), DelegationGeneration: 1}},
		{{NetworkId: [16]byte{10}, DelegationHeadHash: "0x" + head, DelegationGeneration: 1}},
		{{NetworkId: [16]byte{10}, DelegationHeadHash: head[2:], DelegationGeneration: 1}},
		{{NetworkId: [16]byte{10}, DelegationHeadHash: head, DelegationGeneration: 0}},
		{{NetworkId: [16]byte{10}, DelegationHeadHash: head, DelegationGeneration: protocol.MaxWalletMappingHistory + 1}},
	}
	for index, delegations := range cases {
		authority := networkWalletTestAuthority()
		authority.HotkeyDelegations = delegations
		if _, _, err := sign(authority); err == nil {
			t.Errorf("invalid delegation chains %d were signed", index)
		}
	}
	signer, _ := hotkeyDelegationTestSigner(t)
	// v3 without any delegation chain has no canonical form, and neither has an
	// older schema carrying one
	signed, _, err := sign(networkWalletTestAuthority())
	if err != nil {
		t.Fatal(err)
	}
	signed.Schema = WholeWorkAuthorityHotkeyDelegationSchema
	if err := signed.Verify(t.Context(), signer); err == nil {
		t.Fatal("a v3 roster without delegation chains verified")
	}
	authority := networkWalletTestAuthority()
	authority.HotkeyDelegations = []WholeWorkHotkeyDelegation{{NetworkId: [16]byte{10}, DelegationHeadHash: head, DelegationGeneration: 1}}
	signed, _, err = sign(authority)
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{WholeWorkAuthorityNetworkWalletSchema, WholeWorkAuthoritySchema} {
		signed.Schema = schema
		if _, err := signed.SigningDigest(t.Context()); err == nil {
			t.Errorf("a %s roster carrying a delegation chain has a canonical form", schema)
		}
	}
}
