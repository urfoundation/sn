// Original delegation and global hotkey consent chains exercise hotkey mode,
// the last earning mode, while provider- and network-mode requests keep their
// existing bytes. Keys and identities are synthetic.
package payoutroster

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The hotkey that signs the fixture's delegations and global consent, and
// the coldkey that consent names.
const rosterProducerHotkeyTestHotkeySeed = 111
const rosterProducerHotkeyTestColdkeySeed = 112

// The public key and substrate-context signature over message of a synthetic
// sr25519 seed; an empty message only derives the key.
func rosterProducerHotkeyTestKey(t *testing.T, seed byte, message string) ([32]byte, [64]byte) {
	t.Helper()
	mini, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	if message == "" {
		return mini.Public().Encode(), [64]byte{}
	}
	signature, err := mini.ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(message)))
	if err != nil {
		t.Fatal(err)
	}
	return mini.Public().Encode(), signature.Encode()
}

// A global consent chain of hotkeySeed naming the fixture coldkey. Generation
// g is effective from epoch fromEpoch+g-1 through 99.
func rosterProducerHotkeyTestConsents(t *testing.T, fixture *rosterProducerTestFixture, hotkeySeed byte, fromEpoch uint64, generations int) ([]protocol.HotkeyWalletMappingConsent, [][32]byte) {
	t.Helper()
	hotkey, _ := rosterProducerHotkeyTestKey(t, hotkeySeed, "")
	coldkey, _ := rosterProducerHotkeyTestKey(t, rosterProducerHotkeyTestColdkeySeed, "")
	var originals []protocol.HotkeyWalletMappingConsent
	var hashes [][32]byte
	var previous [32]byte
	for index := 0; index < generations; index++ {
		statement := protocol.HotkeyWalletMappingStatement{Schema: protocol.HotkeyWalletMappingConsentSchema, Scope: protocol.HotkeyWalletMappingScope, Subnet: fixture.config.Domain.HotkeySubnet(), Hotkey: hotkey, Coldkey: coldkey, Generation: uint64(index + 1), PreviousHash: previous, Nonce: [32]byte{hotkeySeed, byte(index + 1)}, IssuedAt: fixture.input.Clock.StartTime.Unix() - 900, FromEpoch: fromEpoch + uint64(index), ThroughEpoch: 99}
		message, err := statement.Message()
		if err != nil {
			t.Fatal(err)
		}
		_, coldkeySignature := rosterProducerHotkeyTestKey(t, rosterProducerHotkeyTestColdkeySeed, message)
		_, hotkeySignature := rosterProducerHotkeyTestKey(t, hotkeySeed, message)
		original := protocol.HotkeyWalletMappingConsent{Message: message, ColdkeySignature: coldkeySignature, HotkeySignature: hotkeySignature}
		_, hash, err := protocol.VerifyHotkeyWalletMappingConsent(t.Context(), original)
		if err != nil {
			t.Fatal(err)
		}
		originals, hashes, previous = append(originals, original), append(hashes, hash), hash
	}
	return originals, hashes
}

// Every field is synthetic and the issuance precedes the fixture's earning cut.
func rosterProducerHotkeyTestStatement(t *testing.T, fixture *rosterProducerTestFixture, networkId [16]byte, consentHead [32]byte) protocol.HotkeyNetworkDelegationStatement {
	t.Helper()
	hotkey, _ := rosterProducerHotkeyTestKey(t, rosterProducerHotkeyTestHotkeySeed, "")
	return protocol.HotkeyNetworkDelegationStatement{
		Domain: fixture.config.Domain, UserId: [16]byte{121}, NetworkId: networkId, Hotkey: hotkey,
		ConsentHeadHash: consentHead, ConsentGeneration: 1, Generation: 1, Nonce: [32]byte{122},
		IssuedAt: fixture.input.Clock.StartTime.Unix() - 600, ExpiresAt: fixture.input.Clock.StartTime.Unix() - 300,
		FromEpoch: 8, ThroughEpoch: 99,
	}
}

// Operator and hotkey signatures cover the exact delegation transcript.
func rosterProducerHotkeyTestDelegation(t *testing.T, statement *protocol.HotkeyNetworkDelegationStatement, root *ecdsa.PrivateKey, boundary protocol.ClientKeyEffectiveBoundary) (protocol.WalletMappingConsent, [32]byte) {
	t.Helper()
	if err := protocol.SignProspectiveHotkeyNetworkDelegation(statement, boundary, root); err != nil {
		t.Fatal(err)
	}
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	_, signature := rosterProducerHotkeyTestKey(t, rosterProducerHotkeyTestHotkeySeed, message)
	original := protocol.WalletMappingConsent{Message: message, Signature: signature}
	_, hash, err := protocol.VerifyHotkeyNetworkDelegation(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, hash
}

// The approved hotkey evidence of the fixture's delegated network.
type rosterProducerHotkeyTestEvidence struct {
	statement      protocol.HotkeyNetworkDelegationStatement
	delegationHash [32]byte
	consents       []protocol.HotkeyWalletMappingConsent
	consentHashes  [][32]byte
	hotkey         [32]byte
	coldkey        [32]byte
}

// The provider has no own chain and its network's consent is reviewed absent;
// one delegation and the global chain it names supply its wallet.
func newRosterProducerHotkeyTestFixture(t *testing.T) (*rosterProducerTestFixture, rosterProducerHotkeyTestEvidence) {
	t.Helper()
	fixture := newRosterProducerTestFixture(t)
	evidence := rosterProducerHotkeyTestEvidence{}
	evidence.consents, evidence.consentHashes = rosterProducerHotkeyTestConsents(t, fixture, rosterProducerHotkeyTestHotkeySeed, 8, 1)
	evidence.statement = rosterProducerHotkeyTestStatement(t, fixture, fixture.input.Providers[0].NetworkId, evidence.consentHashes[0])
	var delegation protocol.WalletMappingConsent
	delegation, evidence.delegationHash = rosterProducerHotkeyTestDelegation(t, &evidence.statement, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{123}})
	evidence.hotkey, _ = rosterProducerHotkeyTestKey(t, rosterProducerHotkeyTestHotkeySeed, "")
	evidence.coldkey, _ = rosterProducerHotkeyTestKey(t, rosterProducerHotkeyTestColdkeySeed, "")
	fixture.input.HotkeyDelegations = []HotkeyDelegationInput{{NetworkId: evidence.statement.NetworkId, NetworkWalletAbsent: true, DelegationConsents: []protocol.WalletMappingConsent{delegation}, HotkeyConsents: evidence.consents}}
	return fixture, evidence
}

// The selection is the hotkey's global consent through the fixture delegation.
func rosterProducerHotkeyTestAssertSelection(t *testing.T, selection WalletSelection, evidence rosterProducerHotkeyTestEvidence, clientId [16]byte, networkId [16]byte) {
	t.Helper()
	wallet := selection.Wallet
	if selection.ClientId != clientId || selection.NetworkId != networkId || selection.Unavailable || wallet == nil || wallet.Mode != protocol.EarningWalletModeHotkey || wallet.ClientId != clientId || wallet.NetworkId != networkId || wallet.Coldkey != evidence.coldkey || wallet.Hotkey != evidence.hotkey {
		t.Fatal("reviewed hotkey selection differs from the approved originals", selection)
	}
	if wallet.OriginalHash != evidence.delegationHash || wallet.HeadHash != evidence.delegationHash || wallet.Generation != 1 || wallet.HeadGeneration != 1 || wallet.ConsentOriginalHash != evidence.consentHashes[0] || wallet.ConsentHeadHash != evidence.consentHashes[0] || wallet.ConsentGeneration != 1 || wallet.ConsentHeadGeneration != 1 {
		t.Fatal("reviewed hotkey selection lost its delegation or global consent", wallet)
	}
}

// A provider without an own chain in a network without a network consent
// publishes a signed v3 roster with one delegation head; neither its own head
// nor a network head is invented.
func TestPrepareHotkeyDelegationOnlySignsAndPublishesV3(t *testing.T) {
	fixture, evidence := newRosterProducerHotkeyTestFixture(t)
	raw, request := rosterProducerTestPrepare(t, fixture)
	if request.Authority.Schema != payoutartifact.WholeWorkAuthorityHotkeyDelegationSchema || len(request.Authority.HotkeyDelegations) != 1 || len(request.Authority.NetworkWallets) != 0 || len(request.Authority.ExpectedProviders) != 1 {
		t.Fatal("hotkey-only earning did not derive a complete v3 roster", request.Authority)
	}
	delegation := request.Authority.HotkeyDelegations[0]
	if delegation.NetworkId != evidence.statement.NetworkId || delegation.DelegationHeadHash != hex.EncodeToString(evidence.delegationHash[:]) || delegation.DelegationGeneration != 1 {
		t.Fatal("delegation head differs from its signed original", delegation)
	}
	provider := request.Authority.ExpectedProviders[0]
	if provider.WalletHeadHash != "" || provider.WalletGeneration != 0 || len(request.WalletSelections) != 1 {
		t.Fatal("hotkey fallback was misrepresented as a provider consent", provider, request.WalletSelections)
	}
	rosterProducerHotkeyTestAssertSelection(t, request.WalletSelections[0], evidence, provider.ClientId, provider.NetworkId)
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	publicationCalls := 0
	var publishedOriginal []byte
	result, err := executeWithPublisher(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), true, func(ctx context.Context, original []byte, signer common.Address) error {
		publicationCalls++
		signed, err := payoutartifact.DecodeWholeWorkAuthority(ctx, original, signer)
		if err != nil || signer != fixture.config.AuthoritySigner || signed.Schema != payoutartifact.WholeWorkAuthorityHotkeyDelegationSchema || len(signed.HotkeyDelegations) != 1 || signed.HotkeyDelegations[0] != delegation {
			t.Fatal("publication lost the signed v3 delegation head", signed, err)
		}
		publishedOriginal = bytes.Clone(original)
		return nil
	})
	if err != nil || !result.Published || publicationCalls != 1 || result.AuthoritySha256 != rosterProducerTestHash(publishedOriginal) {
		t.Fatal("hotkey-only roster did not publish its exact retained original", result, publicationCalls, err)
	}
	retained, err := rosterProducerTestLoad(t, fixture)
	if err != nil || !bytes.Equal(retained, publishedOriginal) {
		t.Fatal("published v3 roster differs from retained authority", err)
	}
}

// Provider and network consents keep precedence over a delegation of the
// same network; only a network consent with no effective era falls back.
func TestPrepareHotkeyFallsBackOnlyPastNetworkConsent(t *testing.T) {
	fixture, evidence := newRosterProducerHotkeyTestFixture(t)
	networkId := fixture.input.Providers[0].NetworkId
	fixture.input.Owners = append(fixture.input.Owners, rosterProducerTestOwner(t, fixture, 12, 21, 32, 42))
	fixture.input.Providers = append(fixture.input.Providers, ProviderInput{ClientId: [16]byte{12}, NetworkId: networkId, WalletConsents: []protocol.WalletMappingConsent{}})
	own := rosterProducerTestStatement(fixture)
	ownOriginal, _ := rosterProducerTestConsent(t, &own, 93, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{94}})
	fixture.input.Providers[0].WalletConsents = []protocol.WalletMappingConsent{ownOriginal}
	for _, c := range []struct {
		fromEpoch uint64
		mode      string
	}{{fromEpoch: 8, mode: protocol.EarningWalletModeNetwork}, {fromEpoch: fixture.input.Epoch + 1, mode: protocol.EarningWalletModeHotkey}} {
		network := rosterProducerNetworkTestStatement(fixture, networkId)
		network.FromEpoch = c.fromEpoch
		boundary := protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{104}}
		if c.fromEpoch > fixture.input.Epoch {
			boundary = protocol.ClientKeyEffectiveBoundary{Epoch: fixture.input.Epoch, Block: 98, Hash: [32]byte{104}}
		}
		networkOriginal, networkHash := rosterProducerNetworkTestConsent(t, &network, 103, fixture.registrationKey, boundary)
		fixture.input.NetworkWallets = []NetworkWalletInput{{NetworkId: networkId, WalletConsents: []protocol.WalletMappingConsent{networkOriginal}}}
		fixture.input.HotkeyDelegations[0].NetworkWalletAbsent = false
		_, request := rosterProducerTestPrepare(t, fixture)
		if request.Authority.Schema != payoutartifact.WholeWorkAuthorityHotkeyDelegationSchema || len(request.Authority.NetworkWallets) != 1 || request.Authority.NetworkWallets[0].WalletHeadHash != hex.EncodeToString(networkHash[:]) || len(request.Authority.HotkeyDelegations) != 1 || len(request.WalletSelections) != 2 {
			t.Fatal("a roster with both chain kinds lost a head or a provider", c.mode, request.Authority)
		}
		if selection := request.WalletSelections[0]; selection.Unavailable || selection.Wallet == nil || selection.Wallet.Mode != protocol.EarningWalletModeProvider || selection.Wallet.Coldkey != own.Coldkey {
			t.Fatal("a delegation displaced the provider's effective own wallet", c.mode, selection)
		}
		selection := request.WalletSelections[1]
		if c.mode == protocol.EarningWalletModeHotkey {
			rosterProducerHotkeyTestAssertSelection(t, selection, evidence, [16]byte{12}, networkId)
		} else if selection.Unavailable || selection.Wallet == nil || selection.Wallet.Mode != protocol.EarningWalletModeNetwork || selection.Wallet.Coldkey != network.Coldkey || selection.Wallet.Hotkey != ([32]byte{}) {
			t.Fatal("a delegation displaced the effective network consent", selection)
		}
	}
}

// A network consent whose acceptance may cross the earning start is unknown,
// and unknown is not permission to pay the hotkey's wallet instead.
func TestPrepareUnknownNetworkConsentDoesNotFallBackToHotkey(t *testing.T) {
	fixture, _ := newRosterProducerHotkeyTestFixture(t)
	networkId := fixture.input.Providers[0].NetworkId
	network := rosterProducerNetworkTestStatement(fixture, networkId)
	network.IssuedAt, network.ExpiresAt = fixture.input.Clock.StartTime.Unix()-60, fixture.input.Clock.StartTime.Unix()+240
	networkOriginal, _ := rosterProducerNetworkTestConsent(t, &network, 103, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{104}})
	fixture.input.NetworkWallets = []NetworkWalletInput{{NetworkId: networkId, WalletConsents: []protocol.WalletMappingConsent{networkOriginal}}}
	fixture.input.HotkeyDelegations[0].NetworkWalletAbsent = false
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.WalletSelections) != 1 || !request.WalletSelections[0].Unavailable || request.WalletSelections[0].Wallet != nil {
		t.Fatal("an unknown network consent fell back to the hotkey wallet", request.WalletSelections)
	}
}

// Missing evidence at either earlier mode cannot authorize hotkey fallback:
// every provider states its own chain, and every delegated network states its
// network chain, as supplied or as reviewed absent.
func TestPrepareHotkeyFallbackRequiresExplicitEarlierChains(t *testing.T) {
	fixture, _ := newRosterProducerHotkeyTestFixture(t)
	fixture.input.Providers[0].WalletConsents = nil
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, protocol.ErrWalletMappingUnavailable) || len(prepared) != 0 {
		t.Fatal("a missing own history became authority for hotkey fallback", err)
	}
	fixture, _ = newRosterProducerHotkeyTestFixture(t)
	fixture.input.HotkeyDelegations[0].NetworkWalletAbsent = false
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, protocol.ErrWalletMappingUnavailable) || len(prepared) != 0 {
		t.Fatal("an unstated network chain became authority for hotkey fallback", err)
	}
	fixture, _ = newRosterProducerHotkeyTestFixture(t)
	network := rosterProducerNetworkTestStatement(fixture, fixture.input.Providers[0].NetworkId)
	networkOriginal, _ := rosterProducerNetworkTestConsent(t, &network, 103, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{104}})
	fixture.input.NetworkWallets = []NetworkWalletInput{{NetworkId: network.NetworkId, WalletConsents: []protocol.WalletMappingConsent{networkOriginal}}}
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, protocol.ErrWalletMappingIntegrity) || len(prepared) != 0 {
		t.Fatal("a supplied network chain was also asserted absent", err)
	}
}

// A future delegation rotation remains pinned as the head without replacing
// the earlier delegation, or the global consent head it names, at the epoch.
func TestPrepareFutureDelegationHeadPreservesEarlierSelection(t *testing.T) {
	fixture, evidence := newRosterProducerHotkeyTestFixture(t)
	consents, consentHashes := rosterProducerHotkeyTestConsents(t, fixture, rosterProducerHotkeyTestHotkeySeed, 8, 2)
	first := rosterProducerHotkeyTestStatement(t, fixture, evidence.statement.NetworkId, consentHashes[0])
	firstOriginal, firstHash := rosterProducerHotkeyTestDelegation(t, &first, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{123}})
	second := first
	second.Generation, second.PreviousHash, second.FromEpoch = 2, firstHash, fixture.input.Epoch+1
	second.ConsentHeadHash, second.ConsentGeneration = consentHashes[1], 2
	second.Nonce[0]++
	second.IssuedAt, second.ExpiresAt = fixture.input.Clock.StartTime.Unix()+60, fixture.input.Clock.StartTime.Unix()+300
	secondOriginal, headHash := rosterProducerHotkeyTestDelegation(t, &second, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: fixture.input.Epoch, Block: 150, Hash: [32]byte{124}})
	fixture.input.HotkeyDelegations[0].DelegationConsents = []protocol.WalletMappingConsent{firstOriginal, secondOriginal}
	fixture.input.HotkeyDelegations[0].HotkeyConsents = consents[:1]
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.Authority.HotkeyDelegations) != 1 || request.Authority.HotkeyDelegations[0].DelegationHeadHash != hex.EncodeToString(headHash[:]) || request.Authority.HotkeyDelegations[0].DelegationGeneration != 2 || len(request.WalletSelections) != 1 {
		t.Fatal("future delegation rotation lost its final pinned head", request)
	}
	wallet := request.WalletSelections[0].Wallet
	if request.WalletSelections[0].Unavailable || wallet == nil || wallet.Mode != protocol.EarningWalletModeHotkey || wallet.OriginalHash != firstHash || wallet.HeadHash != headHash || wallet.Generation != 1 || wallet.HeadGeneration != 2 || wallet.ConsentHeadHash != consentHashes[0] || wallet.ConsentHeadGeneration != 1 {
		t.Fatal("future delegation head rewrote the earlier effective selection", wallet)
	}
	// the global chain ends at the head the effective delegation names, not at
	// the future delegation's
	fixture.input.HotkeyDelegations[0].HotkeyConsents = consents
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, protocol.ErrWalletMappingIntegrity) || len(prepared) != 0 {
		t.Fatal("a global chain beyond the effective delegation's head was retained", err)
	}
}

// Unavailable hotkey earning remains an explicit provider outcome, with the
// full delegation head retained for replay.
func TestPrepareRetainsUnavailableHotkeySelections(t *testing.T) {
	for index, mutate := range []func(*rosterProducerTestFixture, *protocol.HotkeyNetworkDelegationStatement, *[]protocol.HotkeyWalletMappingConsent){
		// no delegation effective at the epoch names any global consent
		func(fixture *rosterProducerTestFixture, statement *protocol.HotkeyNetworkDelegationStatement, consents *[]protocol.HotkeyWalletMappingConsent) {
			statement.FromEpoch = fixture.input.Epoch + 1
			*consents = []protocol.HotkeyWalletMappingConsent{}
		},
		func(fixture *rosterProducerTestFixture, statement *protocol.HotkeyNetworkDelegationStatement, consents *[]protocol.HotkeyWalletMappingConsent) {
			statement.ThroughEpoch = fixture.input.Epoch - 1
			*consents = nil
		},
		// the delegation's acceptance may cross the earning start
		func(fixture *rosterProducerTestFixture, statement *protocol.HotkeyNetworkDelegationStatement, _ *[]protocol.HotkeyWalletMappingConsent) {
			statement.IssuedAt, statement.ExpiresAt = fixture.input.Clock.StartTime.Unix()-60, fixture.input.Clock.StartTime.Unix()+240
		},
		// the global consent the delegation names is not effective at the epoch
		func(fixture *rosterProducerTestFixture, statement *protocol.HotkeyNetworkDelegationStatement, consents *[]protocol.HotkeyWalletMappingConsent) {
			later, hashes := rosterProducerHotkeyTestConsents(t, fixture, rosterProducerHotkeyTestHotkeySeed, fixture.input.Epoch+1, 1)
			statement.ConsentHeadHash, *consents = hashes[0], later
		},
	} {
		fixture, evidence := newRosterProducerHotkeyTestFixture(t)
		statement := rosterProducerHotkeyTestStatement(t, fixture, evidence.statement.NetworkId, evidence.consentHashes[0])
		consents := evidence.consents
		mutate(fixture, &statement, &consents)
		original, hash := rosterProducerHotkeyTestDelegation(t, &statement, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{123}})
		fixture.input.HotkeyDelegations[0].DelegationConsents = []protocol.WalletMappingConsent{original}
		fixture.input.HotkeyDelegations[0].HotkeyConsents = consents
		_, request := rosterProducerTestPrepare(t, fixture)
		if len(request.WalletSelections) != 1 || !request.WalletSelections[0].Unavailable || request.WalletSelections[0].Wallet != nil {
			t.Fatal("unavailable hotkey evidence gained an earning wallet", index, request.WalletSelections)
		}
		if len(request.Authority.HotkeyDelegations) != 1 || request.Authority.HotkeyDelegations[0].DelegationHeadHash != hex.EncodeToString(hash[:]) {
			t.Fatal("unavailable hotkey evidence lost its retained head", index, request.Authority)
		}
	}
}

// A supplied delegation must carry its full chain and exactly the global chain
// its effective delegation names, from the pinned root, for its own network.
func TestPrepareRefusesIncompleteOrForeignHotkeyEvidence(t *testing.T) {
	for index, mutate := range []func(*testing.T, *rosterProducerTestFixture, rosterProducerHotkeyTestEvidence){
		// the effective delegation's global chain is missing
		func(_ *testing.T, fixture *rosterProducerTestFixture, _ rosterProducerHotkeyTestEvidence) {
			fixture.input.HotkeyDelegations[0].HotkeyConsents = []protocol.HotkeyWalletMappingConsent{}
		},
		// a global chain of another hotkey
		func(t *testing.T, fixture *rosterProducerTestFixture, _ rosterProducerHotkeyTestEvidence) {
			other, _ := rosterProducerHotkeyTestConsents(t, fixture, 113, 8, 1)
			fixture.input.HotkeyDelegations[0].HotkeyConsents = other
		},
		// a global chain no effective delegation names
		func(t *testing.T, fixture *rosterProducerTestFixture, evidence rosterProducerHotkeyTestEvidence) {
			statement := evidence.statement
			statement.FromEpoch = fixture.input.Epoch + 1
			original, _ := rosterProducerHotkeyTestDelegation(t, &statement, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: fixture.input.Epoch, Block: 98, Hash: [32]byte{123}})
			fixture.input.HotkeyDelegations[0].DelegationConsents = []protocol.WalletMappingConsent{original}
		},
		// a delegation chain without its first generation, or without any
		func(t *testing.T, fixture *rosterProducerTestFixture, evidence rosterProducerHotkeyTestEvidence) {
			second := evidence.statement
			second.Generation, second.PreviousHash, second.FromEpoch = 2, evidence.delegationHash, fixture.input.Epoch+1
			second.Nonce[0]++
			original, _ := rosterProducerHotkeyTestDelegation(t, &second, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: fixture.input.Epoch, Block: 150, Hash: [32]byte{124}})
			fixture.input.HotkeyDelegations[0].DelegationConsents = []protocol.WalletMappingConsent{original}
		},
		func(_ *testing.T, fixture *rosterProducerTestFixture, _ rosterProducerHotkeyTestEvidence) {
			fixture.input.HotkeyDelegations[0].DelegationConsents = []protocol.WalletMappingConsent{}
		},
		// another operator root, another network and another domain
		func(t *testing.T, fixture *rosterProducerTestFixture, evidence rosterProducerHotkeyTestEvidence) {
			statement := evidence.statement
			original, _ := rosterProducerHotkeyTestDelegation(t, &statement, rosterProducerTestKey(t, 52), protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{123}})
			fixture.input.HotkeyDelegations[0].DelegationConsents = []protocol.WalletMappingConsent{original}
		},
		func(t *testing.T, fixture *rosterProducerTestFixture, evidence rosterProducerHotkeyTestEvidence) {
			statement := evidence.statement
			statement.NetworkId[0]++
			original, _ := rosterProducerHotkeyTestDelegation(t, &statement, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{123}})
			fixture.input.HotkeyDelegations[0].DelegationConsents = []protocol.WalletMappingConsent{original}
		},
		func(t *testing.T, fixture *rosterProducerTestFixture, evidence rosterProducerHotkeyTestEvidence) {
			statement := evidence.statement
			statement.Domain.NoID++
			original, _ := rosterProducerHotkeyTestDelegation(t, &statement, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{123}})
			fixture.input.HotkeyDelegations[0].DelegationConsents = []protocol.WalletMappingConsent{original}
		},
		// a network consent in place of a delegation
		func(t *testing.T, fixture *rosterProducerTestFixture, _ rosterProducerHotkeyTestEvidence) {
			network := rosterProducerNetworkTestStatement(fixture, fixture.input.Providers[0].NetworkId)
			original, _ := rosterProducerNetworkTestConsent(t, &network, 103, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{104}})
			fixture.input.HotkeyDelegations[0].DelegationConsents = []protocol.WalletMappingConsent{original}
		},
	} {
		fixture, evidence := newRosterProducerHotkeyTestFixture(t)
		mutate(t, fixture, evidence)
		if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); err == nil || len(prepared) != 0 {
			t.Errorf("incomplete or foreign hotkey evidence %d gained reviewed authority", index)
		}
	}
}

// A delegated network needs an expected provider and occurs once.
func TestPrepareRefusesDuplicateAndUnusedHotkeyDelegations(t *testing.T) {
	fixture, _ := newRosterProducerHotkeyTestFixture(t)
	fixture.input.HotkeyDelegations = append(fixture.input.HotkeyDelegations, fixture.input.HotkeyDelegations[0])
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, protocol.ErrWalletMappingIntegrity) || len(prepared) != 0 {
		t.Fatal("a duplicate delegation gained reviewed authority", err)
	}
	fixture, evidence := newRosterProducerHotkeyTestFixture(t)
	statement := rosterProducerHotkeyTestStatement(t, fixture, [16]byte{22}, evidence.consentHashes[0])
	original, _ := rosterProducerHotkeyTestDelegation(t, &statement, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{123}})
	fixture.input.HotkeyDelegations = append(fixture.input.HotkeyDelegations, HotkeyDelegationInput{NetworkId: statement.NetworkId, NetworkWalletAbsent: true, DelegationConsents: []protocol.WalletMappingConsent{original}, HotkeyConsents: evidence.consents})
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, protocol.ErrWalletMappingIntegrity) || len(prepared) != 0 {
		t.Fatal("a delegation without an expected provider gained payout authority", err)
	}
}

// Delegated networks share one head per network, sorted by network, while
// each provider keeps its own selection.
func TestPrepareHotkeyDelegationsShareHeadsAndSortNetworks(t *testing.T) {
	fixture, evidence := newRosterProducerHotkeyTestFixture(t)
	fixture.input.Owners = append(fixture.input.Owners,
		rosterProducerTestOwner(t, fixture, 12, 21, 32, 42),
		rosterProducerTestOwner(t, fixture, 13, 20, 33, 43),
	)
	fixture.input.Providers = []ProviderInput{
		{ClientId: [16]byte{13}, NetworkId: [16]byte{20}, WalletConsents: []protocol.WalletMappingConsent{}},
		{ClientId: [16]byte{12}, NetworkId: [16]byte{21}, WalletConsents: []protocol.WalletMappingConsent{}},
		fixture.input.Providers[0],
	}
	statement := rosterProducerHotkeyTestStatement(t, fixture, [16]byte{20}, evidence.consentHashes[0])
	original, secondHash := rosterProducerHotkeyTestDelegation(t, &statement, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{125}})
	fixture.input.HotkeyDelegations = append(fixture.input.HotkeyDelegations, HotkeyDelegationInput{NetworkId: statement.NetworkId, NetworkWalletAbsent: true, DelegationConsents: []protocol.WalletMappingConsent{original}, HotkeyConsents: evidence.consents})
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.Authority.HotkeyDelegations) != 2 || len(request.Authority.ExpectedProviders) != 3 || len(request.WalletSelections) != 3 {
		t.Fatal("shared delegation duplicated a head or removed a provider", request.Authority)
	}
	for index, want := range []payoutartifact.WholeWorkHotkeyDelegation{
		{NetworkId: [16]byte{20}, DelegationHeadHash: hex.EncodeToString(secondHash[:]), DelegationGeneration: 1},
		{NetworkId: [16]byte{21}, DelegationHeadHash: hex.EncodeToString(evidence.delegationHash[:]), DelegationGeneration: 1},
	} {
		if request.Authority.HotkeyDelegations[index] != want {
			t.Fatal("delegation heads were not sorted by their independent identities", index, request.Authority.HotkeyDelegations)
		}
	}
	for index, selection := range request.WalletSelections {
		provider := request.Authority.ExpectedProviders[index]
		if selection.ClientId != provider.ClientId || selection.Unavailable || selection.Wallet == nil || selection.Wallet.Mode != protocol.EarningWalletModeHotkey || selection.Wallet.ClientId != provider.ClientId || selection.Wallet.NetworkId != provider.NetworkId || selection.Wallet.Coldkey != evidence.coldkey {
			t.Fatal("shared-delegation review selection lost its provider association", index, selection)
		}
	}
}

// The preview is reviewed metadata, so a new digest cannot authorize swapping
// any hotkey field independently of the retained originals.
func TestExecuteRefusesChangedHotkeySelection(t *testing.T) {
	for index, change := range []func(*protocol.EarningWallet){
		func(wallet *protocol.EarningWallet) { wallet.Coldkey[0]++ },
		func(wallet *protocol.EarningWallet) { wallet.Hotkey[0]++ },
		func(wallet *protocol.EarningWallet) { wallet.ConsentHeadHash[0]++ },
		func(wallet *protocol.EarningWallet) { wallet.ConsentGeneration++ },
	} {
		fixture, _ := newRosterProducerHotkeyTestFixture(t)
		_, request := rosterProducerTestPrepare(t, fixture)
		if len(request.WalletSelections) != 1 || request.WalletSelections[0].Wallet == nil {
			t.Fatal("fixture did not derive an effective hotkey wallet")
		}
		change(request.WalletSelections[0].Wallet)
		changed := rosterProducerTestJson(t, request)
		publicationCalls := 0
		if _, err := executeWithPublisher(t.Context(), fixture.config, changed, rosterProducerTestHash(changed), true, func(context.Context, []byte, common.Address) error {
			publicationCalls++
			return nil
		}); err == nil || publicationCalls != 0 {
			t.Fatal("changed hotkey review selection gained signing admission", index, err)
		}
		rosterProducerTestAssertNoCustody(t, fixture)
	}
}

// Provider- and network-mode review wallets encode exactly as the earning
// wallet did before hotkey mode, so Execute's byte comparison of rederived
// requests holds for every existing v1 and v2 request.
func TestWalletSelectionKeepsProviderAndNetworkModeBytes(t *testing.T) {
	previous := func(wallet protocol.EarningWallet) any {
		return struct {
			Mode           string
			ClientId       [16]byte
			NetworkId      [16]byte
			Coldkey        [32]byte
			OriginalHash   [32]byte
			Generation     uint64
			HeadHash       [32]byte
			HeadGeneration uint64
		}{Mode: wallet.Mode, ClientId: wallet.ClientId, NetworkId: wallet.NetworkId, Coldkey: wallet.Coldkey, OriginalHash: wallet.OriginalHash, Generation: wallet.Generation, HeadHash: wallet.HeadHash, HeadGeneration: wallet.HeadGeneration}
	}
	for _, mode := range []string{protocol.EarningWalletModeProvider, protocol.EarningWalletModeNetwork} {
		wallet := protocol.EarningWallet{Mode: mode, ClientId: [16]byte{1}, NetworkId: [16]byte{2}, Coldkey: [32]byte{3}, OriginalHash: [32]byte{4}, Generation: 5, HeadHash: [32]byte{6}, HeadGeneration: 7}
		selection := WalletSelection{ClientId: wallet.ClientId, NetworkId: wallet.NetworkId, Wallet: &wallet}
		if !bytes.Equal(rosterProducerTestJson(t, selection), rosterProducerTestJson(t, struct {
			ClientId  [16]byte `json:"client_id"`
			NetworkId [16]byte `json:"network_id"`
			Wallet    any      `json:"wallet"`
		}{ClientId: wallet.ClientId, NetworkId: wallet.NetworkId, Wallet: previous(wallet)})) {
			t.Fatalf("a %s-mode review selection changed its bytes", mode)
		}
	}
	// a prepared v2 request names no hotkey field, and a v1 request is
	// unchanged by an explicitly empty delegation list
	fixture, _, _ := newRosterProducerNetworkTestFixture(t)
	raw, request := rosterProducerTestPrepare(t, fixture)
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		t.Fatal(err)
	}
	if request.Authority.Schema != payoutartifact.WholeWorkAuthorityNetworkWalletSchema || bytes.Contains(raw, []byte(`"hotkey_delegations"`)) || bytes.Contains(raw, []byte(`"Hotkey"`)) || bytes.Contains(raw, []byte(`"Consent`)) || members["wallet_selections"] == nil {
		t.Fatal("a v2 request gained hotkey fields", request.Authority.Schema)
	}
	fixture = newRosterProducerTestFixture(t)
	first, _ := rosterProducerTestPrepare(t, fixture)
	fixture.input.HotkeyDelegations = []HotkeyDelegationInput{}
	second, secondRequest := rosterProducerTestPrepare(t, fixture)
	if !bytes.Equal(first, second) || secondRequest.Authority.Schema != payoutartifact.WholeWorkAuthoritySchema || bytes.Contains(first, []byte(`"hotkey_delegations"`)) {
		t.Fatal("an empty delegation input changed the canonical v1 request")
	}
}
