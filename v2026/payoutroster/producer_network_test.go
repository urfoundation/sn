// Original network and provider consent chains exercise reviewed earning-wallet
// precedence while the existing provider-only request format stays unchanged.
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

// Every field is synthetic and the issuance precedes the fixture's earning cut.
func rosterProducerNetworkTestStatement(fixture *rosterProducerTestFixture, networkId [16]byte) protocol.NetworkWalletMappingStatement {
	return protocol.NetworkWalletMappingStatement{
		Domain: fixture.config.Domain, UserId: [16]byte{101}, NetworkId: networkId,
		Generation: 1, Nonce: [32]byte{102},
		IssuedAt: fixture.input.Clock.StartTime.Unix() - 600, ExpiresAt: fixture.input.Clock.StartTime.Unix() - 300,
		FromEpoch: 8, ThroughEpoch: 99,
	}
}

// Operator and coldkey signatures cover the exact network-scoped transcript.
func rosterProducerNetworkTestConsent(t *testing.T, statement *protocol.NetworkWalletMappingStatement, seed byte, root *ecdsa.PrivateKey, boundary protocol.ClientKeyEffectiveBoundary) (protocol.WalletMappingConsent, [32]byte) {
	t.Helper()
	mini, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	statement.Coldkey = mini.Public().Encode()
	if err := protocol.SignProspectiveNetworkWalletMapping(statement, boundary, root); err != nil {
		t.Fatal(err)
	}
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := mini.ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(message)))
	if err != nil {
		t.Fatal(err)
	}
	original := protocol.WalletMappingConsent{Message: message, Signature: signature.Encode()}
	_, hash, err := protocol.VerifyNetworkWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, hash
}

// The provider has no own chain; one complete network chain supplies its wallet.
func newRosterProducerNetworkTestFixture(t *testing.T) (*rosterProducerTestFixture, protocol.NetworkWalletMappingStatement, [32]byte) {
	t.Helper()
	fixture := newRosterProducerTestFixture(t)
	statement := rosterProducerNetworkTestStatement(fixture, fixture.input.Providers[0].NetworkId)
	original, hash := rosterProducerNetworkTestConsent(t, &statement, 103, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{104}})
	fixture.input.NetworkWallets = []NetworkWalletInput{{NetworkId: statement.NetworkId, WalletConsents: []protocol.WalletMappingConsent{original}}}
	return fixture, statement, hash
}

// A provider without an own head can publish a signed roster with one separately
// pinned network head; its provider identity never becomes an invented own head.
func TestPrepareNetworkWalletOnlySignsAndPublishesV2(t *testing.T) {
	fixture, statement, hash := newRosterProducerNetworkTestFixture(t)
	raw, request := rosterProducerTestPrepare(t, fixture)
	if request.Authority.Schema != payoutartifact.WholeWorkAuthorityNetworkWalletSchema || len(request.Authority.NetworkWallets) != 1 || len(request.Authority.ExpectedProviders) != 1 {
		t.Fatal("network-only earning did not derive a complete v2 roster", request.Authority)
	}
	network := request.Authority.NetworkWallets[0]
	if network.NetworkId != statement.NetworkId || network.WalletHeadHash != hex.EncodeToString(hash[:]) || network.WalletGeneration != 1 {
		t.Fatal("network head differs from its signed original", network)
	}
	provider := request.Authority.ExpectedProviders[0]
	if provider.ClientId != fixture.input.Providers[0].ClientId || provider.WalletHeadHash != "" || provider.WalletGeneration != 0 {
		t.Fatal("network fallback was misrepresented as a provider consent", provider)
	}
	if len(request.WalletSelections) != 1 {
		t.Fatal("network earning omitted its provider review selection", request.WalletSelections)
	}
	selection := request.WalletSelections[0]
	if selection.ClientId != provider.ClientId || selection.NetworkId != provider.NetworkId || selection.Unavailable || selection.Wallet == nil || selection.Wallet.Mode != protocol.EarningWalletModeNetwork || selection.Wallet.Coldkey != statement.Coldkey || selection.Wallet.OriginalHash != hash || selection.Wallet.HeadHash != hash || selection.Wallet.Generation != 1 || selection.Wallet.HeadGeneration != 1 {
		t.Fatal("reviewed network selection differs from the approved original", selection)
	}
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	publicationCalls := 0
	var publishedOriginal []byte
	result, err := executeWithPublisher(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), true, func(ctx context.Context, original []byte, signer common.Address) error {
		publicationCalls++
		if signer != fixture.config.AuthoritySigner {
			t.Fatal("network roster changed the independently approved signer")
		}
		signed, err := payoutartifact.DecodeWholeWorkAuthority(ctx, original, signer)
		if err != nil || signed.Schema != payoutartifact.WholeWorkAuthorityNetworkWalletSchema || len(signed.NetworkWallets) != 1 || signed.NetworkWallets[0] != network {
			t.Fatal("publication lost the signed v2 network head", signed, err)
		}
		publishedOriginal = bytes.Clone(original)
		return nil
	})
	if err != nil || !result.Published || publicationCalls != 1 || result.AuthoritySha256 != rosterProducerTestHash(publishedOriginal) {
		t.Fatal("network-only roster did not publish its exact retained original", result, publicationCalls, err)
	}
	retained, err := rosterProducerTestLoad(t, fixture)
	if err != nil || !bytes.Equal(retained, publishedOriginal) {
		t.Fatal("published v2 roster differs from retained authority", err)
	}
}

// Shared-network providers use one pinned network head, and independently
// supplied network chains are ordered before the authority is reviewed.
func TestPrepareNetworkWalletsShareHeadsAndSortNetworks(t *testing.T) {
	fixture, firstStatement, firstHash := newRosterProducerNetworkTestFixture(t)
	fixture.input.Owners = append(fixture.input.Owners,
		rosterProducerTestOwner(t, fixture, 12, 21, 32, 42),
		rosterProducerTestOwner(t, fixture, 13, 20, 33, 43),
	)
	fixture.input.Providers = []ProviderInput{
		{ClientId: [16]byte{13}, NetworkId: [16]byte{20}, WalletConsents: []protocol.WalletMappingConsent{}},
		{ClientId: [16]byte{12}, NetworkId: [16]byte{21}, WalletConsents: []protocol.WalletMappingConsent{}},
		fixture.input.Providers[0],
	}
	secondStatement := rosterProducerNetworkTestStatement(fixture, [16]byte{20})
	second, secondHash := rosterProducerNetworkTestConsent(t, &secondStatement, 105, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{106}})
	fixture.input.NetworkWallets = append(fixture.input.NetworkWallets, NetworkWalletInput{NetworkId: secondStatement.NetworkId, WalletConsents: []protocol.WalletMappingConsent{second}})
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.Authority.NetworkWallets) != 2 || len(request.Authority.ExpectedProviders) != 3 {
		t.Fatal("shared network duplicated a head or removed a provider", request.Authority)
	}
	for index, want := range []payoutartifact.WholeWorkNetworkWallet{
		{NetworkId: secondStatement.NetworkId, WalletHeadHash: hex.EncodeToString(secondHash[:]), WalletGeneration: 1},
		{NetworkId: firstStatement.NetworkId, WalletHeadHash: hex.EncodeToString(firstHash[:]), WalletGeneration: 1},
	} {
		if request.Authority.NetworkWallets[index] != want {
			t.Fatal("network heads were not sorted by their independent identities", index, request.Authority.NetworkWallets)
		}
	}
	for index, provider := range request.Authority.ExpectedProviders {
		if provider.ClientId != ([16]byte{byte(11 + index)}) {
			t.Fatal("network preparation changed provider ordering", index, provider)
		}
	}
	if len(request.WalletSelections) != 3 {
		t.Fatal("shared-network providers lost individual review selections", request.WalletSelections)
	}
	for index, selection := range request.WalletSelections {
		wantColdkey, wantHash := firstStatement.Coldkey, firstHash
		if index == 2 {
			wantColdkey, wantHash = secondStatement.Coldkey, secondHash
		}
		provider := request.Authority.ExpectedProviders[index]
		if selection.ClientId != provider.ClientId || selection.NetworkId != provider.NetworkId || selection.Unavailable || selection.Wallet == nil || selection.Wallet.Mode != protocol.EarningWalletModeNetwork || selection.Wallet.ClientId != provider.ClientId || selection.Wallet.NetworkId != provider.NetworkId || selection.Wallet.Coldkey != wantColdkey || selection.Wallet.HeadHash != wantHash {
			t.Fatal("shared-network review selection lost its provider association", index, selection)
		}
	}
}

// Missing evidence cannot authorize fallback. Only an explicit empty own
// history asserts that the provider has no consent chain to take precedence.
func TestPrepareNetworkFallbackRequiresExplicitEmptyProviderHistory(t *testing.T) {
	for _, omitted := range []bool{false, true} {
		fixture, _, _ := newRosterProducerNetworkTestFixture(t)
		_, explicit := rosterProducerTestPrepare(t, fixture)
		if len(explicit.WalletSelections) != 1 || explicit.WalletSelections[0].Unavailable || explicit.WalletSelections[0].Wallet == nil || explicit.WalletSelections[0].Wallet.Mode != protocol.EarningWalletModeNetwork {
			t.Fatal("explicit empty own history did not permit approved network fallback", explicit.WalletSelections)
		}
		var input map[string]json.RawMessage
		if err := json.Unmarshal(rosterProducerTestJson(t, fixture.input), &input); err != nil {
			t.Fatal(err)
		}
		var providers []map[string]json.RawMessage
		if err := json.Unmarshal(input["providers"], &providers); err != nil {
			t.Fatal(err)
		}
		if omitted {
			delete(providers[0], "wallet_consents")
		} else {
			providers[0]["wallet_consents"] = json.RawMessage("null")
		}
		input["providers"] = rosterProducerTestJson(t, providers)
		if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, input)); !errors.Is(err, protocol.ErrWalletMappingUnavailable) || len(prepared) != 0 {
			t.Fatal("missing own history became authority for network fallback", omitted, err)
		}
	}
}

// The stricter network fallback gate does not reinterpret already supported
// provider-only requests whose absent own history was represented by null.
func TestPrepareProviderOnlyNilHistoryKeepsV1Compatibility(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	fixture.input.Providers[0].WalletConsents = nil
	raw, request := rosterProducerTestPrepare(t, fixture)
	if request.Authority.Schema != payoutartifact.WholeWorkAuthoritySchema || len(request.WalletSelections) != 0 || !bytes.Contains(raw, []byte(`"wallet_consents":null`)) {
		t.Fatal("network fallback gate changed an existing v1 null-history request", request)
	}
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	if _, err := Execute(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), false); err != nil {
		t.Fatal("existing v1 null-history request stopped signing", err)
	}
}

// An effective own consent wins while the independently supplied network head
// remains available for the other providers of that network.
func TestPrepareProviderWalletTakesPrecedenceOverNetwork(t *testing.T) {
	fixture, networkStatement, networkHash := newRosterProducerNetworkTestFixture(t)
	statement := rosterProducerTestStatement(fixture)
	original, hash := rosterProducerTestConsent(t, &statement, 93, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{94}})
	fixture.input.Providers[0].WalletConsents = []protocol.WalletMappingConsent{original}
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.WalletSelections) != 1 {
		t.Fatal("own wallet precedence lost its provider selection", request.WalletSelections)
	}
	selection := request.WalletSelections[0]
	if selection.Unavailable || selection.Wallet == nil || selection.Wallet.Mode != protocol.EarningWalletModeProvider || selection.Wallet.Coldkey != statement.Coldkey || selection.Wallet.Coldkey == networkStatement.Coldkey || selection.Wallet.OriginalHash != hash || selection.Wallet.HeadHash != hash {
		t.Fatal("network consent displaced the provider's effective own wallet", selection)
	}
	if len(request.Authority.NetworkWallets) != 1 || request.Authority.NetworkWallets[0].WalletHeadHash != hex.EncodeToString(networkHash[:]) || request.Authority.ExpectedProviders[0].WalletHeadHash != hex.EncodeToString(hash[:]) {
		t.Fatal("selection rewrote the independently pinned consent heads", request.Authority)
	}
}

// Only a fully verified own chain with no effective consent may defer to the
// network; expired and not-yet-effective earning intervals both qualify.
func TestPrepareExpiredAndFutureProviderWalletsUseNetwork(t *testing.T) {
	for _, future := range []bool{false, true} {
		fixture, networkStatement, networkHash := newRosterProducerNetworkTestFixture(t)
		statement := rosterProducerTestStatement(fixture)
		if future {
			statement.FromEpoch = fixture.input.Epoch + 1
		} else {
			statement.ThroughEpoch = fixture.input.Epoch - 1
		}
		original, ownHash := rosterProducerTestConsent(t, &statement, 93, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{94}})
		fixture.input.Providers[0].WalletConsents = []protocol.WalletMappingConsent{original}
		_, request := rosterProducerTestPrepare(t, fixture)
		if len(request.WalletSelections) != 1 {
			t.Fatal("ineffective own consent removed its provider", future, request.WalletSelections)
		}
		selection := request.WalletSelections[0]
		if selection.Unavailable || selection.Wallet == nil || selection.Wallet.Mode != protocol.EarningWalletModeNetwork || selection.Wallet.Coldkey != networkStatement.Coldkey || selection.Wallet.HeadHash != networkHash {
			t.Fatal("verified ineffective own consent blocked the effective network wallet", future, selection)
		}
		if request.Authority.ExpectedProviders[0].WalletHeadHash != hex.EncodeToString(ownHash[:]) || request.Authority.ExpectedProviders[0].WalletGeneration != 1 {
			t.Fatal("network fallback omitted the ineffective own chain's head", future)
		}
	}
}

// Legacy own consent can be effective at the epoch without proving prospective
// acceptance. That uncertainty cannot pay a different network wallet instead.
func TestPrepareLegacyProviderWalletDoesNotFallBackToNetwork(t *testing.T) {
	fixture, _, _ := newRosterProducerNetworkTestFixture(t)
	statement := rosterProducerTestStatement(fixture)
	statement.Schema = protocol.WalletMappingConsentSchema
	mini, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{97})
	if err != nil {
		t.Fatal(err)
	}
	statement.Coldkey = mini.Public().Encode()
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := mini.ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(message)))
	if err != nil {
		t.Fatal(err)
	}
	original := protocol.WalletMappingConsent{Message: message, Signature: signature.Encode()}
	_, hash, err := protocol.VerifyWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.Providers[0].WalletConsents = []protocol.WalletMappingConsent{original}
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.WalletSelections) != 1 || !request.WalletSelections[0].Unavailable || request.WalletSelections[0].Wallet != nil {
		t.Fatal("legacy prospective uncertainty fell back to another wallet", request.WalletSelections)
	}
	if len(request.Authority.ExpectedProviders) != 1 || request.Authority.ExpectedProviders[0].WalletHeadHash != hex.EncodeToString(hash[:]) || len(request.Authority.NetworkWallets) != 1 {
		t.Fatal("unknown earning selection erased independently reviewed heads", request.Authority)
	}
}

// Even v2 own consent stays unknown when acceptance could cross the earning
// start; an available network wallet is not permission to bypass that gate.
func TestPrepareUnknownProspectiveProviderWalletDoesNotFallBack(t *testing.T) {
	fixture, _, _ := newRosterProducerNetworkTestFixture(t)
	statement := rosterProducerTestStatement(fixture)
	statement.IssuedAt, statement.ExpiresAt = fixture.input.Clock.StartTime.Unix()-60, fixture.input.Clock.StartTime.Unix()+240
	original, _ := rosterProducerTestConsent(t, &statement, 93, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{94}})
	fixture.input.Providers[0].WalletConsents = []protocol.WalletMappingConsent{original}
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.WalletSelections) != 1 || !request.WalletSelections[0].Unavailable || request.WalletSelections[0].Wallet != nil {
		t.Fatal("unproved prospective own acceptance fell back to a network wallet", request.WalletSelections)
	}
}

// Missing own originals are not a verified ineffective chain, so a complete
// network history cannot normalize that evidence gap into a payout selection.
func TestPrepareIncompleteProviderHistoryDoesNotFallBackToNetwork(t *testing.T) {
	fixture, _, _ := newRosterProducerNetworkTestFixture(t)
	first := rosterProducerTestStatement(fixture)
	_, firstHash := rosterProducerTestConsent(t, &first, 93, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{94}})
	second := first
	second.Generation, second.PreviousHash, second.FromEpoch = 2, firstHash, fixture.input.Epoch+1
	second.Nonce[0]++
	original, _ := rosterProducerTestConsent(t, &second, 95, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: fixture.input.Epoch, Block: 150, Hash: [32]byte{96}})
	fixture.input.Providers[0].WalletConsents = []protocol.WalletMappingConsent{original}
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); err == nil || len(prepared) != 0 {
		t.Fatal("missing own predecessor fell back to the network wallet", err)
	}
}

// A future network rotation remains pinned as the head without replacing the
// earlier wallet effective at the independently reviewed earning epoch.
func TestPrepareFutureNetworkHeadPreservesEarlierSelection(t *testing.T) {
	fixture, first, firstHash := newRosterProducerNetworkTestFixture(t)
	second := first
	second.Generation, second.PreviousHash, second.FromEpoch = 2, firstHash, fixture.input.Epoch+1
	second.Nonce[0]++
	second.IssuedAt, second.ExpiresAt = fixture.input.Clock.StartTime.Unix()+60, fixture.input.Clock.StartTime.Unix()+300
	original, headHash := rosterProducerNetworkTestConsent(t, &second, 105, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: fixture.input.Epoch, Block: 150, Hash: [32]byte{106}})
	fixture.input.NetworkWallets[0].WalletConsents = append(fixture.input.NetworkWallets[0].WalletConsents, original)
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.Authority.NetworkWallets) != 1 || request.Authority.NetworkWallets[0].WalletHeadHash != hex.EncodeToString(headHash[:]) || request.Authority.NetworkWallets[0].WalletGeneration != 2 || len(request.WalletSelections) != 1 {
		t.Fatal("future network rotation lost its final pinned head", request)
	}
	selection := request.WalletSelections[0]
	if selection.Unavailable || selection.Wallet == nil || selection.Wallet.Mode != protocol.EarningWalletModeNetwork || selection.Wallet.Coldkey != first.Coldkey || selection.Wallet.OriginalHash != firstHash || selection.Wallet.HeadHash != headHash || selection.Wallet.Generation != 1 || selection.Wallet.HeadGeneration != 2 {
		t.Fatal("future network head rewrote the earlier effective wallet", selection)
	}
}

// Unavailable network earning remains an explicit provider outcome, with its
// full head retained for replay rather than hidden from the complete roster.
func TestPrepareRetainsUnavailableNetworkWalletSelections(t *testing.T) {
	for index, mutate := range []func(*rosterProducerTestFixture, *protocol.NetworkWalletMappingStatement){
		func(fixture *rosterProducerTestFixture, statement *protocol.NetworkWalletMappingStatement) {
			statement.ThroughEpoch = fixture.input.Epoch - 1
		},
		func(fixture *rosterProducerTestFixture, statement *protocol.NetworkWalletMappingStatement) {
			statement.FromEpoch = fixture.input.Epoch + 1
		},
		func(fixture *rosterProducerTestFixture, statement *protocol.NetworkWalletMappingStatement) {
			statement.IssuedAt, statement.ExpiresAt = fixture.input.Clock.StartTime.Unix()-60, fixture.input.Clock.StartTime.Unix()+240
		},
	} {
		fixture, statement, _ := newRosterProducerNetworkTestFixture(t)
		mutate(fixture, &statement)
		original, hash := rosterProducerNetworkTestConsent(t, &statement, 103, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{104}})
		fixture.input.NetworkWallets[0].WalletConsents = []protocol.WalletMappingConsent{original}
		_, request := rosterProducerTestPrepare(t, fixture)
		if len(request.WalletSelections) != 1 || !request.WalletSelections[0].Unavailable || request.WalletSelections[0].Wallet != nil {
			t.Fatal("unavailable network consent gained an earning wallet", index, request.WalletSelections)
		}
		if len(request.Authority.ExpectedProviders) != 1 || len(request.Authority.NetworkWallets) != 1 || request.Authority.NetworkWallets[0].WalletHeadHash != hex.EncodeToString(hash[:]) {
			t.Fatal("unavailable network consent lost its provider or retained head", index, request.Authority)
		}
	}
}

// The preview is reviewed metadata, so a new digest cannot authorize swapping
// its wallet independently of the retained original consent chains.
func TestExecuteRefusesChangedNetworkWalletSelection(t *testing.T) {
	fixture, _, _ := newRosterProducerNetworkTestFixture(t)
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.WalletSelections) != 1 || request.WalletSelections[0].Wallet == nil {
		t.Fatal("fixture did not derive an effective network wallet")
	}
	request.WalletSelections[0].Wallet.Coldkey[0]++
	changed := rosterProducerTestJson(t, request)
	publicationCalls := 0
	if _, err := executeWithPublisher(t.Context(), fixture.config, changed, rosterProducerTestHash(changed), true, func(context.Context, []byte, common.Address) error {
		publicationCalls++
		return nil
	}); err == nil {
		t.Fatal("changed review selection gained signing admission")
	}
	if publicationCalls != 0 {
		t.Fatal("changed review selection reached publication", publicationCalls)
	}
	rosterProducerTestAssertNoCustody(t, fixture)
}

// An explicitly duplicated network input remains a contradiction even when
// both copies contain identical signed originals.
func TestPrepareRefusesDuplicateNetworkWallets(t *testing.T) {
	fixture, _, _ := newRosterProducerNetworkTestFixture(t)
	fixture.input.NetworkWallets = append(fixture.input.NetworkWallets, fixture.input.NetworkWallets[0])
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); err == nil || len(prepared) != 0 {
		t.Fatal("duplicate network chain gained reviewed authority", err)
	}
}

// A valid network consent does not authorize adding an unused payout network.
func TestPrepareRefusesUnusedNetworkWallet(t *testing.T) {
	fixture, _, _ := newRosterProducerNetworkTestFixture(t)
	statement := rosterProducerNetworkTestStatement(fixture, [16]byte{22})
	original, _ := rosterProducerNetworkTestConsent(t, &statement, 105, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{106}})
	fixture.input.NetworkWallets = append(fixture.input.NetworkWallets, NetworkWalletInput{NetworkId: statement.NetworkId, WalletConsents: []protocol.WalletMappingConsent{original}})
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); err == nil || len(prepared) != 0 {
		t.Fatal("network without an expected provider gained payout authority", err)
	}
}

// Distinct valid signatures cannot move a network consent to another domain,
// pinned root, or independently declared network identity.
func TestPrepareRefusesNetworkConsentAuthorityMismatch(t *testing.T) {
	for index, mutate := range []func(*rosterProducerTestFixture, *protocol.NetworkWalletMappingStatement) *ecdsa.PrivateKey{
		func(fixture *rosterProducerTestFixture, statement *protocol.NetworkWalletMappingStatement) *ecdsa.PrivateKey {
			statement.Domain.NoID++
			return fixture.registrationKey
		},
		func(fixture *rosterProducerTestFixture, statement *protocol.NetworkWalletMappingStatement) *ecdsa.PrivateKey {
			statement.NetworkId[0]++
			return fixture.registrationKey
		},
		func(_ *rosterProducerTestFixture, _ *protocol.NetworkWalletMappingStatement) *ecdsa.PrivateKey {
			return rosterProducerTestKey(t, 52)
		},
	} {
		fixture, statement, _ := newRosterProducerNetworkTestFixture(t)
		root := mutate(fixture, &statement)
		original, _ := rosterProducerNetworkTestConsent(t, &statement, 103, root, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{104}})
		fixture.input.NetworkWallets[0].WalletConsents = []protocol.WalletMappingConsent{original}
		if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, protocol.ErrWalletMappingIntegrity) || len(prepared) != 0 {
			t.Fatal("signed network identity contradicted independent approval", index, err)
		}
	}
}

// A correct selected mapping and final head cannot excuse a foreign root in
// an earlier original or an intermediate future network generation.
func TestPrepareRefusesUnselectedNetworkConsentRoot(t *testing.T) {
	for _, changedIndex := range []int{0, 2} {
		fixture, _, _ := newRosterProducerNetworkTestFixture(t)
		foreignKey := rosterProducerTestKey(t, 52)
		history := func(foreignIndex int) []protocol.WalletMappingConsent {
			originals := []protocol.WalletMappingConsent{}
			var previousHash [32]byte
			for index, fromEpoch := range []uint64{7, 8, 10, 11} {
				statement := rosterProducerNetworkTestStatement(fixture, fixture.input.Providers[0].NetworkId)
				statement.Generation, statement.PreviousHash, statement.FromEpoch = uint64(index+1), previousHash, fromEpoch
				statement.Nonce[0] += byte(index)
				boundary := protocol.ClientKeyEffectiveBoundary{Epoch: fromEpoch - 1, Block: 98, Hash: [32]byte{byte(111 + index)}}
				if fromEpoch > fixture.input.Epoch {
					boundary.Block = uint64(150 + index)
					statement.IssuedAt, statement.ExpiresAt = fixture.input.Clock.StartTime.Unix()+600, fixture.input.Clock.StartTime.Unix()+900
				}
				root := fixture.registrationKey
				if index == foreignIndex {
					root = foreignKey
				}
				original, hash := rosterProducerNetworkTestConsent(t, &statement, byte(103+index), root, boundary)
				originals = append(originals, original)
				previousHash = hash
			}
			return originals
		}
		fixture.input.NetworkWallets[0].WalletConsents = history(-1)
		rosterProducerTestPrepare(t, fixture)
		fixture.input.NetworkWallets[0].WalletConsents = history(changedIndex)
		if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, protocol.ErrWalletMappingIntegrity) || len(prepared) != 0 {
			t.Fatal("unselected network consent borrowed a foreign operator root", changedIndex, err)
		}
	}
}

// A supplied head must carry every original back to its first generation.
func TestPrepareRefusesIncompleteNetworkWalletHistory(t *testing.T) {
	for _, empty := range []bool{false, true} {
		fixture, firstStatement, firstHash := newRosterProducerNetworkTestFixture(t)
		secondStatement := firstStatement
		secondStatement.Generation, secondStatement.PreviousHash, secondStatement.FromEpoch = 2, firstHash, fixture.input.Epoch+1
		secondStatement.Nonce[0]++
		second, _ := rosterProducerNetworkTestConsent(t, &secondStatement, 105, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: fixture.input.Epoch, Block: 150, Hash: [32]byte{106}})
		fixture.input.NetworkWallets[0].WalletConsents = []protocol.WalletMappingConsent{second}
		if empty {
			fixture.input.NetworkWallets[0].WalletConsents = []protocol.WalletMappingConsent{}
		}
		if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); err == nil || len(prepared) != 0 {
			t.Fatal("incomplete network consent history gained a pinned head", empty, err)
		}
	}
}

// Omitted and explicitly empty optional network inputs preserve the old request
// and signed authority schemas without adding fields to their canonical bytes.
func TestPrepareWithoutNetworkWalletsPreservesV1Request(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	first, firstRequest := rosterProducerTestPrepare(t, fixture)
	fixture.input.NetworkWallets = []NetworkWalletInput{}
	second, secondRequest := rosterProducerTestPrepare(t, fixture)
	if !bytes.Equal(first, second) || firstRequest.Authority.Schema != payoutartifact.WholeWorkAuthoritySchema || secondRequest.Authority.Schema != payoutartifact.WholeWorkAuthoritySchema || bytes.Contains(first, []byte(`"network_wallets"`)) {
		t.Fatal("optional empty network input changed the canonical v1 request")
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(first, &members); err != nil {
		t.Fatal(err)
	}
	if len(members) != 3 || members["schema"] == nil || members["input"] == nil || members["authority"] == nil {
		t.Fatal("review metadata changed the existing prepared request contract", members)
	}
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	if _, err := Execute(t.Context(), fixture.config, first, rosterProducerTestHash(first), false); err != nil {
		t.Fatal("provider-only request stopped signing after network support", err)
	}
	original, err := rosterProducerTestLoad(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := payoutartifact.DecodeWholeWorkAuthority(t.Context(), original, fixture.config.AuthoritySigner)
	if err != nil || signed.Schema != payoutartifact.WholeWorkAuthoritySchema || bytes.Contains(original, []byte(`"network_wallets"`)) {
		t.Fatal("provider-only signed authority changed its existing format", err)
	}
}
