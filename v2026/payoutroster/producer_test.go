// Synthetic signed originals exercise the producer's complete population,
// reviewed request, and durable signing boundaries without a live endpoint.
package payoutroster

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Private keys, identity bytes, headers, and deployment values are synthetic.
// Construction leaves the key absent and private custody directories empty.
type rosterProducerTestFixture struct {
	config          Config
	input           Input
	authorityKey    *ecdsa.PrivateKey
	registrationKey *ecdsa.PrivateKey
}

// Fixed test-only scalars avoid any dependency on external key custody.
func rosterProducerTestKey(t testing.TB, seed byte) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.ToECDSA(bytes.Repeat([]byte{seed}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// Header originals authenticate both times before any consent is selected.
func newRosterProducerTestFixture(t *testing.T) *rosterProducerTestFixture {
	t.Helper()
	directory := t.TempDir()
	fixture := &rosterProducerTestFixture{
		authorityKey:    rosterProducerTestKey(t, 61),
		registrationKey: rosterProducerTestKey(t, 51),
	}
	artifactKey := rosterProducerTestKey(t, 71)
	requestKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{81}, ed25519.SeedSize))
	fixture.config = Config{
		Schema: ConfigSchema,
		Domain: protocol.ClientKeyHistoryDomain{
			ChainID: 1401, GenesisHash: [32]byte{1}, Netuid: 701,
			Coordinator: common.Address{2}, SettlementVault: common.Address{3},
			DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 6,
		},
		RequestPublicKey:    [32]byte(requestKey[ed25519.SeedSize:]),
		AuthoritySigner:     crypto.PubkeyToAddress(fixture.authorityKey.PublicKey),
		ArtifactSigner:      crypto.PubkeyToAddress(artifactKey.PublicKey),
		ClientKeyRootSigner: crypto.PubkeyToAddress(fixture.registrationKey.PublicKey),
		ApiBase:             "https://roster-api.example",
		KeyFile:             filepath.Join(directory, "authority.key"),
		StateDirectory:      filepath.Join(directory, "state"),
		InboxDirectory:      filepath.Join(directory, "inbox"),
	}
	for _, path := range []string{fixture.config.StateDirectory, fixture.config.InboxDirectory} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Unix(1800000000, 0).UTC()
	end := start.Add(time.Hour)
	startHeader := &types.Header{Number: big.NewInt(100), Time: uint64(start.Unix())}
	endHeader := &types.Header{Number: big.NewInt(200), Time: uint64(end.Unix())}
	clock := &payoutartifact.ClosedWorkWindowClock{
		Start:     payoutartifact.Boundary{Number: 100, Hash: startHeader.Hash().Hex()},
		End:       payoutartifact.Boundary{Number: 200, Hash: endHeader.Hash().Hex()},
		StartTime: start, EndTime: end,
	}
	var err error
	clock.StartHeader, err = rlp.EncodeToBytes(startHeader)
	if err != nil {
		t.Fatal(err)
	}
	clock.EndHeader, err = rlp.EncodeToBytes(endHeader)
	if err != nil {
		t.Fatal(err)
	}
	fixture.input = Input{
		Schema: InputSchema, Complete: true, Epoch: 9, Clock: clock,
		Owners: []OwnerInput{}, Providers: []ProviderInput{},
		PriorContracts: []payoutartifact.WholeWorkPriorContract{},
		WorkSources:    []protocol.ProviderWorkSourceAuthority{},
	}
	fixture.input.Owners = append(fixture.input.Owners, rosterProducerTestOwner(t, fixture, 11, 21, 31, 41))
	fixture.input.Providers = append(fixture.input.Providers, ProviderInput{ClientId: [16]byte{11}, NetworkId: [16]byte{21}, WalletConsents: []protocol.WalletMappingConsent{}})
	return fixture
}

// Enrollment and registration use separate real signatures over the same key.
func rosterProducerTestOwner(t *testing.T, fixture *rosterProducerTestFixture, client, network, generation, seed byte) OwnerInput {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	domainHash, err := fixture.config.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := coreprotocol.SignOriginalWorkOwnerEnrollment(t.Context(), coreprotocol.OriginalWorkOwnerEnrollment{
		DomainHash: domainHash, ClientId: [16]byte{client}, Generation: [16]byte{generation},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := enrollment.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	registration := protocol.ClientKeyRegistration{
		Domain: fixture.config.Domain, ClientID: enrollment.ClientId, NetworkID: [16]byte{network},
		Generation: 1, Present: true, PublicKey: enrollment.PublicKey,
		EffectiveBoundary: protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{99}},
	}
	var prior *protocol.ClientKeyRegistration
	for _, owner := range fixture.input.Owners {
		if owner.Registration.ClientID == registration.ClientID && (prior == nil || prior.Generation < owner.Registration.Generation) {
			value := owner.Registration
			prior = &value
		}
	}
	if prior != nil {
		registration.Generation = prior.Generation + 1
		registration.PreviousHash, err = prior.ContentHash()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := protocol.SignClientKeyRegistration(&registration, fixture.registrationKey); err != nil {
		t.Fatal(err)
	}
	if err := registration.Follows(prior); err != nil {
		t.Fatal(err)
	}
	return OwnerInput{Enrollment: raw, Registration: registration}
}

// Test mutations still pass through the public bounded JSON entry points.
func rosterProducerTestJson(t testing.TB, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Return both the reviewed exact bytes and their inspectable public projection.
func rosterProducerTestPrepare(t *testing.T, fixture *rosterProducerTestFixture) ([]byte, Request) {
	t.Helper()
	raw, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input))
	if err != nil {
		t.Fatal(err)
	}
	var request Request
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	return raw, request
}

// The independent review binds the exact request file, including originals.
func rosterProducerTestHash(raw []byte) string {
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// The producer must authenticate these bytes against its separate signer pin.
func rosterProducerTestWriteKey(t testing.TB, path string, key *ecdsa.PrivateKey) {
	t.Helper()
	raw := []byte(hex.EncodeToString(crypto.FromECDSA(key)))
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// Close custody before executing again so restart assertions use a new owner.
func rosterProducerTestLoad(t *testing.T, fixture *rosterProducerTestFixture) ([]byte, error) {
	t.Helper()
	store, err := OpenStore(t.Context(), fixture.config.StateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	domainHash, err := fixture.config.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	raw, loadErr := store.Load(t.Context(), domainHash, fixture.input.Epoch)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return raw, loadErr
}

// Early refusals must leave the already provisioned custody directory empty.
func rosterProducerTestAssertNoCustody(t testing.TB, fixture *rosterProducerTestFixture) {
	t.Helper()
	entries, err := os.ReadDir(fixture.config.StateDirectory)
	if err != nil || len(entries) != 0 {
		t.Fatal("request refusal reached durable signing state", entries, err)
	}
}

// Absence of a population cannot be normalized into reviewed known-empty work.
func TestPrepareRequiresExplicitCompletePopulation(t *testing.T) {
	for _, field := range []string{"complete", "owners", "providers", "prior_contracts", "clock"} {
		for _, omitted := range []bool{false, true} {
			fixture := newRosterProducerTestFixture(t)
			var input map[string]json.RawMessage
			if err := json.Unmarshal(rosterProducerTestJson(t, fixture.input), &input); err != nil {
				t.Fatal(err)
			}
			if omitted {
				delete(input, field)
			} else if field == "complete" {
				input[field] = json.RawMessage("false")
			} else {
				input[field] = json.RawMessage("null")
			}
			if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, input)); err == nil || len(prepared) != 0 {
				t.Fatalf("missing complete population gained a request: field=%s omitted=%t err=%v", field, omitted, err)
			}
		}
	}
}

// An independently asserted empty population is a valid complete epoch.
func TestPrepareRetainsExplicitEmptyPopulation(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	fixture.input.Owners, fixture.input.Providers = []OwnerInput{}, []ProviderInput{}
	_, request := rosterProducerTestPrepare(t, fixture)
	if request.Authority.Owners == nil || request.Authority.ExpectedProviders == nil || request.Authority.PriorContracts == nil || len(request.Authority.Owners) != 0 || len(request.Authority.ExpectedProviders) != 0 || request.Authority.Epoch != fixture.input.Epoch {
		t.Fatal("explicitly empty population was lost", request.Authority)
	}
}

// Preparation neither opens the absent key nor creates durable signing state.
func TestPrepareIsOfflineWithoutSigningKey(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	for _, path := range []string{fixture.config.StateDirectory, fixture.config.InboxDirectory} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := rosterProducerTestPrepare(t, fixture)
	second, _ := rosterProducerTestPrepare(t, fixture)
	if !bytes.Equal(first, second) {
		t.Fatal("unchanged originals produced different reviewed bytes")
	}
	for _, path := range []string{fixture.config.KeyFile, fixture.config.StateDirectory, fixture.config.InboxDirectory} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("offline preparation touched signing custody", path, err)
		}
	}
}

// Multiple SDK lifecycles of one client remain sorted, explicit owners.
func TestPrepareRetainsDistinctOwnerGenerations(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	older := fixture.input.Owners[0]
	newer := rosterProducerTestOwner(t, fixture, 11, 21, 32, 42)
	fixture.input.Owners = []OwnerInput{newer, older}
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.Authority.Owners) != 2 || request.Authority.Owners[0].Generation != ([16]byte{31}) || request.Authority.Owners[1].Generation != ([16]byte{32}) || request.Authority.Owners[0].PublicKey == request.Authority.Owners[1].PublicKey {
		t.Fatal("separate SDK generations were collapsed or reordered", request.Authority.Owners)
	}
}

// Repeating one signed lifecycle cannot create an apparent second participant.
func TestPrepareRefusesDuplicateOwnerGeneration(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	fixture.input.Owners = append(fixture.input.Owners, fixture.input.Owners[0])
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, payoutartifact.ErrClosedWorkIntegrity) || len(prepared) != 0 {
		t.Fatal("duplicate owner lifecycle gained authority", err)
	}
}

// A valid signature under another root is still outside the pinned authority.
func TestPrepareRefusesDifferentRegistrationRoot(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	foreignKey := rosterProducerTestKey(t, 52)
	if err := protocol.SignClientKeyRegistration(&fixture.input.Owners[0].Registration, foreignKey); err != nil {
		t.Fatal(err)
	}
	if err := fixture.input.Owners[0].Registration.VerifySignature(); err != nil {
		t.Fatal("fixture must retain a valid foreign signature", err)
	}
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); err == nil || len(prepared) != 0 {
		t.Fatal("foreign registration root gained population authority", err)
	}
}

// Independent signatures must agree on domain, client, and public key presence.
func TestPrepareRefusesRegistrationEnrollmentMismatch(t *testing.T) {
	for index, mutate := range []func(*protocol.ClientKeyRegistration){
		func(value *protocol.ClientKeyRegistration) { value.Domain.NoID++ },
		func(value *protocol.ClientKeyRegistration) { value.ClientID[0]++ },
		func(value *protocol.ClientKeyRegistration) { value.PublicKey[0]++ },
		func(value *protocol.ClientKeyRegistration) { value.Present, value.PublicKey = false, [32]byte{} },
	} {
		fixture := newRosterProducerTestFixture(t)
		registration := &fixture.input.Owners[0].Registration
		mutate(registration)
		if err := protocol.SignClientKeyRegistration(registration, fixture.registrationKey); err != nil {
			t.Fatal(index, err)
		}
		if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); err == nil || len(prepared) != 0 {
			t.Fatal("independently signed identity mismatch gained authority", index, err)
		}
	}
}

// A declared provider cannot silently move away from its registered network.
func TestPrepareRefusesProviderNetworkMismatch(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	fixture.input.Providers[0].NetworkId[0]++
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, payoutartifact.ErrClosedWorkIntegrity) || len(prepared) != 0 {
		t.Fatal("provider borrowed a different registered network", err)
	}
}

// Caller times cannot replace physical raw headers when selecting consents.
func TestPrepareRefusesChangedOriginalClock(t *testing.T) {
	for index, mutate := range []func(*payoutartifact.ClosedWorkWindowClock){
		func(clock *payoutartifact.ClosedWorkWindowClock) { clock.StartHeader = nil },
		func(clock *payoutartifact.ClosedWorkWindowClock) { clock.StartTime = clock.StartTime.Add(time.Second) },
		func(clock *payoutartifact.ClosedWorkWindowClock) {
			clock.EndHeader = append(bytes.Clone(clock.EndHeader), 0)
		},
	} {
		fixture := newRosterProducerTestFixture(t)
		mutate(fixture.input.Clock)
		if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, payoutartifact.ErrClosedWorkUnavailable) || len(prepared) != 0 {
			t.Fatal("unsigned clock declaration gained an earning boundary", index, err)
		}
	}
}

// Duplicate members are refused even when they would decode to the same value.
func TestPrepareRefusesDuplicateJsonFields(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	raw := rosterProducerTestJson(t, fixture.input)
	for index, changed := range [][]byte{
		bytes.Replace(raw, []byte(`"complete":true`), []byte(`"complete":true,"complete":true`), 1),
		bytes.Replace(raw, []byte(`"generation":1`), []byte(`"generation":1,"generation":1`), 1),
	} {
		if bytes.Equal(raw, changed) {
			t.Fatal("fixture failed to add the intended duplicate", index)
		}
		if prepared, err := Prepare(t.Context(), fixture.config, changed); err == nil || len(prepared) != 0 {
			t.Fatal("duplicate JSON field gained reviewed bytes", index, err)
		}
	}
}

// A fresh review digest does not authorize tampering with the derived roster.
func TestExecuteRefusesChangedCanonicalAuthority(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	_, request := rosterProducerTestPrepare(t, fixture)
	request.Authority.ExpectedProviders = []payoutartifact.WholeWorkExpectedProvider{}
	changed := rosterProducerTestJson(t, request)
	if _, err := Execute(t.Context(), fixture.config, changed, rosterProducerTestHash(changed), false); err == nil || !strings.Contains(err.Error(), "canonical derivation") {
		t.Fatal("modified unsigned roster gained signing admission", err)
	}
	rosterProducerTestAssertNoCustody(t, fixture)
}

// Equivalent whitespace and duplicate members cannot replace reviewed bytes.
func TestExecuteRefusesNoncanonicalRequest(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	raw, _ := rosterProducerTestPrepare(t, fixture)
	for index, changed := range [][]byte{
		append([]byte(" "), raw...),
		bytes.Replace(raw, []byte(`"complete":true`), []byte(`"complete":true,"complete":true`), 1),
	} {
		if _, err := Execute(t.Context(), fixture.config, changed, rosterProducerTestHash(changed), false); err == nil {
			t.Fatal("alternate request spelling gained signing admission", index)
		}
	}
	rosterProducerTestAssertNoCustody(t, fixture)
}

// The digest supplied by independent review is checked before opening custody.
func TestExecuteRefusesDifferentReviewDigest(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	raw, _ := rosterProducerTestPrepare(t, fixture)
	if _, err := Execute(t.Context(), fixture.config, raw, strings.Repeat("0", 64), false); err == nil || !strings.Contains(err.Error(), "independently reviewed") {
		t.Fatal("unreviewed request reached signing admission", err)
	}
	rosterProducerTestAssertNoCustody(t, fixture)
}

// First execution needs a physical dedicated key even for sign-only operation.
func TestExecuteRefusesMissingSigningKey(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	raw, _ := rosterProducerTestPrepare(t, fixture)
	if result, err := Execute(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), false); !errors.Is(err, os.ErrNotExist) || result.AuthoritySha256 != "" || result.Published {
		t.Fatal("missing key produced a signed authority", result, err)
	}
	if original, err := rosterProducerTestLoad(t, fixture); !errors.Is(err, os.ErrNotExist) || len(original) != 0 {
		t.Fatal("missing key retained an authority", err)
	}
}

// A parseable private scalar cannot substitute its owner for the approved pin.
func TestExecuteRefusesDifferentSigningKey(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.registrationKey)
	raw, _ := rosterProducerTestPrepare(t, fixture)
	if result, err := Execute(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), false); err == nil || !strings.Contains(err.Error(), "independently pinned authority") || result.AuthoritySha256 != "" || result.Published {
		t.Fatal("different private key substituted a roster signer", result, err)
	}
	if original, err := rosterProducerTestLoad(t, fixture); !errors.Is(err, os.ErrNotExist) || len(original) != 0 {
		t.Fatal("different private key retained an authority", err)
	}
}

// A shared private-key file remains refused before any signature is retained.
func TestExecuteRefusesUnsafeSigningKey(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	if err := os.Chmod(fixture.config.KeyFile, 0644); err != nil {
		t.Fatal(err)
	}
	raw, _ := rosterProducerTestPrepare(t, fixture)
	if result, err := Execute(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), false); err == nil || result.AuthoritySha256 != "" || result.Published {
		t.Fatal("unsafe private-key custody produced an authority", result, err)
	}
	if original, err := rosterProducerTestLoad(t, fixture); !errors.Is(err, os.ErrNotExist) || len(original) != 0 {
		t.Fatal("unsafe private-key custody retained an authority", err)
	}
}

// New execution owners reuse exact signed bytes after the private key is gone.
func TestExecuteSignOnlyRetainsStableOriginalAcrossRestart(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	raw, request := rosterProducerTestPrepare(t, fixture)
	first, err := executeWithPublisher(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), false, func(context.Context, []byte, common.Address) error {
		t.Fatal("sign-only execution reached publication")
		return nil
	})
	if err != nil || first.Published || first.AuthoritySha256 == "" {
		t.Fatal("sign-only execution failed to retain authority", first, err)
	}
	original, err := rosterProducerTestLoad(t, fixture)
	if err != nil || first.AuthoritySha256 != rosterProducerTestHash(original) {
		t.Fatal("result did not name the retained original", first, err)
	}
	signed, err := payoutartifact.DecodeWholeWorkAuthority(t.Context(), original, fixture.config.AuthoritySigner)
	if err != nil {
		t.Fatal(err)
	}
	signed.Signature = [65]byte{}
	if !bytes.Equal(rosterProducerTestJson(t, signed), rosterProducerTestJson(t, request.Authority)) {
		t.Fatal("signing changed the reviewed authority")
	}
	if err := os.Remove(fixture.config.KeyFile); err != nil {
		t.Fatal(err)
	}
	second, err := Execute(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), false)
	if err != nil || first != second {
		t.Fatal("restart needed the deleted key or changed its result", first, second, err)
	}
	retained, err := rosterProducerTestLoad(t, fixture)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("restart changed signed original bytes", err)
	}
}

// An independently re-reviewed different population cannot replace one epoch.
func TestExecuteRefusesChangedPopulationForRetainedEpoch(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	raw, _ := rosterProducerTestPrepare(t, fixture)
	if _, err := Execute(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), false); err != nil {
		t.Fatal(err)
	}
	original, err := rosterProducerTestLoad(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.Providers = []ProviderInput{}
	changed, _ := rosterProducerTestPrepare(t, fixture)
	if bytes.Equal(raw, changed) {
		t.Fatal("fixture did not change the epoch population")
	}
	publicationCalls := 0
	if _, err := executeWithPublisher(t.Context(), fixture.config, changed, rosterProducerTestHash(changed), true, func(context.Context, []byte, common.Address) error {
		publicationCalls++
		return nil
	}); !errors.Is(err, ErrStoreConflict) {
		t.Fatal("same epoch accepted a different reviewed roster", err)
	}
	if publicationCalls != 0 {
		t.Fatal("conflicting request reached publication", publicationCalls)
	}
	retained, err := rosterProducerTestLoad(t, fixture)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("conflicting request changed retained authority", err)
	}
}

// The callback itself observes custody, then a lost acknowledgement forces the
// next execution owner to publish the same bytes without loading a private key.
func TestExecuteRetainsBeforePublicationAndRetriesOriginal(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	raw, _ := rosterProducerTestPrepare(t, fixture)
	domainHash, err := fixture.config.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	requestName, err := storeName(domainHash, fixture.input.Epoch, "request.json")
	if err != nil {
		t.Fatal(err)
	}
	authorityName, err := storeName(domainHash, fixture.input.Epoch, "authority.json")
	if err != nil {
		t.Fatal(err)
	}
	publishedName, err := storeName(domainHash, fixture.input.Epoch, "published")
	if err != nil {
		t.Fatal(err)
	}
	publicationCalls := 0
	var firstOriginal []byte
	lostAcknowledgement := errors.New("synthetic acknowledgement interrupted")
	publishOriginal := func(ctx context.Context, original []byte, signer common.Address) error {
		publicationCalls++
		if ctx.Err() != nil || signer != fixture.config.AuthoritySigner {
			t.Fatal("publication changed its admitted owner", signer, ctx.Err())
		}
		retainedRequest, err := os.ReadFile(filepath.Join(fixture.config.StateDirectory, requestName))
		if err != nil || !bytes.Equal(retainedRequest, raw) {
			t.Fatal("publication preceded exact request retention", err)
		}
		retainedOriginal, err := os.ReadFile(filepath.Join(fixture.config.StateDirectory, authorityName))
		if err != nil || !bytes.Equal(retainedOriginal, original) {
			t.Fatal("publication preceded exact signed original retention", err)
		}
		if _, err := payoutartifact.DecodeWholeWorkAuthority(ctx, original, signer); err != nil {
			t.Fatal("publication received an invalid retained signature", err)
		}
		if _, err := os.Lstat(filepath.Join(fixture.config.StateDirectory, publishedName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("publication was acknowledged before its callback returned", err)
		}
		if publicationCalls == 1 {
			firstOriginal = bytes.Clone(original)
			return lostAcknowledgement
		}
		if !bytes.Equal(firstOriginal, original) {
			t.Fatal("retry replaced the already published signed original")
		}
		return nil
	}
	first, err := executeWithPublisher(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), true, publishOriginal)
	if !errors.Is(err, lostAcknowledgement) || first.Published || first.AuthoritySha256 != rosterProducerTestHash(firstOriginal) || publicationCalls != 1 {
		t.Fatal("interrupted acknowledgement lost exact retained custody", first, publicationCalls, err)
	}
	if err := os.Remove(fixture.config.KeyFile); err != nil {
		t.Fatal(err)
	}
	second, err := executeWithPublisher(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), true, publishOriginal)
	if err != nil || !second.Published || second.AuthoritySha256 != first.AuthoritySha256 || second.RequestSha256 != first.RequestSha256 || publicationCalls != 2 {
		t.Fatal("restart failed to publish the original without its key", first, second, publicationCalls, err)
	}
	third, err := executeWithPublisher(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), true, publishOriginal)
	if err != nil || second != third || publicationCalls != 2 {
		t.Fatal("retained acknowledgement did not suppress repeat publication", second, third, publicationCalls, err)
	}
}

// A retained reviewed request and matching acknowledgement permit exact
// deterministic restoration of a missing authority without another publication.
func TestExecuteRestoresAcknowledgedOriginalWithoutRepublishing(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	raw, _ := rosterProducerTestPrepare(t, fixture)
	publicationCalls := 0
	var publishedOriginal []byte
	publishOriginal := func(_ context.Context, original []byte, signer common.Address) error {
		publicationCalls++
		if signer != fixture.config.AuthoritySigner {
			t.Fatal("publication changed its independently pinned signer")
		}
		publishedOriginal = bytes.Clone(original)
		return nil
	}
	first, err := executeWithPublisher(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), true, publishOriginal)
	if err != nil || !first.Published || publicationCalls != 1 {
		t.Fatal("fixture failed to retain an acknowledged original", first, publicationCalls, err)
	}
	original := bytes.Clone(publishedOriginal)
	domainHash, err := fixture.config.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	name, err := storeName(domainHash, fixture.input.Epoch, "authority.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(fixture.config.StateDirectory, name)); err != nil {
		t.Fatal(err)
	}
	second, err := executeWithPublisher(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), true, publishOriginal)
	if err != nil || second != first || publicationCalls != 1 {
		t.Fatal("restoring an acknowledged original changed or republished it", first, second, publicationCalls, err)
	}
	retained, err := rosterProducerTestLoad(t, fixture)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("restoration did not recover exact signed original bytes", err)
	}
}

// Existing signed custody decides which missing review file may be restored.
// A rejected replacement must not poison that empty slot before comparison.
func TestExecuteMissingRequestRefusesDifferentReviewBeforeRetention(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	raw, _ := rosterProducerTestPrepare(t, fixture)
	first, err := Execute(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), false)
	if err != nil {
		t.Fatal(err)
	}
	original, err := rosterProducerTestLoad(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	domainHash, err := fixture.config.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	name, err := storeName(domainHash, fixture.input.Epoch, "request.json")
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(fixture.config.StateDirectory, name)
	if err := os.Remove(requestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.config.KeyFile); err != nil {
		t.Fatal(err)
	}
	fixture.input.Providers = []ProviderInput{}
	changed, _ := rosterProducerTestPrepare(t, fixture)
	publicationCalls := 0
	if _, err := executeWithPublisher(t.Context(), fixture.config, changed, rosterProducerTestHash(changed), true, func(context.Context, []byte, common.Address) error {
		publicationCalls++
		return nil
	}); err == nil {
		t.Fatal("different review replaced a missing request beside retained authority")
	}
	if publicationCalls != 0 {
		t.Fatal("rejected replacement reached publication", publicationCalls)
	}
	if _, err := os.Lstat(requestPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected replacement poisoned the missing review slot", err)
	}
	second, err := Execute(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), false)
	if err != nil || second != first {
		t.Fatal("correct review could not restore its missing file without a key", first, second, err)
	}
	retainedRequest, err := os.ReadFile(requestPath)
	if err != nil || !bytes.Equal(raw, retainedRequest) {
		t.Fatal("restored review differs from its exact original", err)
	}
	retainedOriginal, err := rosterProducerTestLoad(t, fixture)
	if err != nil || !bytes.Equal(original, retainedOriginal) {
		t.Fatal("review restoration changed retained signed authority", err)
	}
}

// A surviving acknowledgement pins restoration even when both earlier custody
// files are gone. A different review cannot consume either missing slot.
func TestExecuteSurvivingAcknowledgementRejectsDifferentRecovery(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	raw, _ := rosterProducerTestPrepare(t, fixture)
	publicationCalls := 0
	var publishedOriginal []byte
	publishOriginal := func(_ context.Context, original []byte, _ common.Address) error {
		publicationCalls++
		publishedOriginal = bytes.Clone(original)
		return nil
	}
	first, err := executeWithPublisher(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), true, publishOriginal)
	if err != nil || !first.Published || publicationCalls != 1 {
		t.Fatal("fixture did not retain its original acknowledgement", first, publicationCalls, err)
	}
	original := bytes.Clone(publishedOriginal)
	domainHash, err := fixture.config.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	requestName, err := storeName(domainHash, fixture.input.Epoch, "request.json")
	if err != nil {
		t.Fatal(err)
	}
	authorityName, err := storeName(domainHash, fixture.input.Epoch, "authority.json")
	if err != nil {
		t.Fatal(err)
	}
	publishedName, err := storeName(domainHash, fixture.input.Epoch, "published")
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(fixture.config.StateDirectory, requestName)
	authorityPath := filepath.Join(fixture.config.StateDirectory, authorityName)
	publishedPath := filepath.Join(fixture.config.StateDirectory, publishedName)
	acknowledgement, err := os.ReadFile(publishedPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{requestPath, authorityPath} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	fixture.input.Providers = []ProviderInput{}
	changed, _ := rosterProducerTestPrepare(t, fixture)
	if _, err := executeWithPublisher(t.Context(), fixture.config, changed, rosterProducerTestHash(changed), true, publishOriginal); !errors.Is(err, ErrStoreConflict) {
		t.Fatal("different recovery ignored the surviving acknowledgement", err)
	}
	if publicationCalls != 1 {
		t.Fatal("rejected recovery reached publication", publicationCalls)
	}
	for _, path := range []string{requestPath, authorityPath} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("rejected recovery poisoned a missing custody slot", path, err)
		}
	}
	retainedAcknowledgement, err := os.ReadFile(publishedPath)
	if err != nil || !bytes.Equal(acknowledgement, retainedAcknowledgement) {
		t.Fatal("rejected recovery changed the surviving acknowledgement", err)
	}
	second, err := executeWithPublisher(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), true, publishOriginal)
	if err != nil || first != second || publicationCalls != 1 {
		t.Fatal("correct recovery failed or republished an acknowledged original", first, second, publicationCalls, err)
	}
	retainedRequest, err := os.ReadFile(requestPath)
	if err != nil || !bytes.Equal(raw, retainedRequest) {
		t.Fatal("correct recovery changed original reviewed request bytes", err)
	}
	retainedOriginal, err := rosterProducerTestLoad(t, fixture)
	if err != nil || !bytes.Equal(original, retainedOriginal) {
		t.Fatal("correct recovery changed original signed authority bytes", err)
	}
}

// Physical authority custody failure is forced before the publisher boundary.
func TestExecuteStorageFailureNeverPublishes(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	rosterProducerTestWriteKey(t, fixture.config.KeyFile, fixture.authorityKey)
	raw, _ := rosterProducerTestPrepare(t, fixture)
	domainHash, err := fixture.config.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	name, err := storeName(domainHash, fixture.input.Epoch, "authority.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(fixture.config.StateDirectory, name), 0700); err != nil {
		t.Fatal(err)
	}
	publicationCalls := 0
	result, err := executeWithPublisher(t.Context(), fixture.config, raw, rosterProducerTestHash(raw), true, func(context.Context, []byte, common.Address) error {
		publicationCalls++
		return nil
	})
	if !errors.Is(err, ErrStoreIntegrity) || publicationCalls != 0 || result.Published || result.AuthoritySha256 != "" {
		t.Fatal("failed custody reached publication", result, publicationCalls, err)
	}
}

// Cancellation is an explicit state transition before preparation or key access.
func TestProducerRefusesCanceledOwner(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	raw, _ := rosterProducerTestPrepare(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if prepared, err := Prepare(ctx, fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, context.Canceled) || len(prepared) != 0 {
		t.Fatal("canceled owner prepared authority", err)
	}
	if _, err := Execute(ctx, fixture.config, raw, rosterProducerTestHash(raw), false); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled owner reached signing", err)
	}
	rosterProducerTestAssertNoCustody(t, fixture)
}

// Both signatures are actual protocol transcripts over the exact same mapping.
func rosterProducerTestConsent(t *testing.T, statement *protocol.WalletMappingStatement, seed byte, root *ecdsa.PrivateKey, boundary protocol.ClientKeyEffectiveBoundary) (protocol.WalletMappingConsent, [32]byte) {
	t.Helper()
	mini, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	statement.Coldkey = mini.Public().Encode()
	if err := protocol.SignProspectiveWalletMapping(statement, boundary, root); err != nil {
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
	_, hash, err := protocol.VerifyWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, hash
}

// The default consent predates the fixture's earning epoch and start block.
func rosterProducerTestStatement(fixture *rosterProducerTestFixture) protocol.WalletMappingStatement {
	provider := fixture.input.Providers[0]
	return protocol.WalletMappingStatement{
		Domain: fixture.config.Domain, UserId: [16]byte{91}, ClientId: provider.ClientId,
		NetworkId: provider.NetworkId, Generation: 1, Nonce: [32]byte{92},
		IssuedAt: fixture.input.Clock.StartTime.Unix() - 600, ExpiresAt: fixture.input.Clock.StartTime.Unix() - 300,
		FromEpoch: 8, ThroughEpoch: 99,
	}
}

// Pinning a later consent must preserve the earlier mapping at an earned epoch.
func TestPrepareFutureConsentHeadPreservesEarlierMapping(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	first := rosterProducerTestStatement(fixture)
	old, oldHash := rosterProducerTestConsent(t, &first, 93, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{94}})
	second := first
	second.Generation, second.PreviousHash, second.FromEpoch = 2, oldHash, fixture.input.Epoch+1
	second.Nonce[0]++
	second.IssuedAt, second.ExpiresAt = fixture.input.Clock.StartTime.Unix()+60, fixture.input.Clock.StartTime.Unix()+300
	newer, newHash := rosterProducerTestConsent(t, &second, 95, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: fixture.input.Epoch, Block: 150, Hash: [32]byte{96}})
	fixture.input.Providers[0].WalletConsents = []protocol.WalletMappingConsent{old, newer}
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.Authority.ExpectedProviders) != 1 {
		t.Fatal("provider disappeared after a future rotation")
	}
	provider := request.Authority.ExpectedProviders[0]
	if provider.WalletHeadHash != hex.EncodeToString(newHash[:]) || provider.WalletGeneration != 2 {
		t.Fatal("reviewed roster lost the complete future head", provider)
	}
	mapping, err := protocol.VerifyWalletMappingHistory(t.Context(), request.Input.Providers[0].WalletConsents, protocol.WalletMappingHistoryExpectation{
		Domain: request.Authority.Domain, ClientId: provider.ClientId, HeadHash: newHash, Generation: provider.WalletGeneration, Epoch: request.Authority.Epoch,
	})
	if err != nil || mapping == nil || mapping.OriginalHash != oldHash || mapping.Statement.Coldkey != first.Coldkey {
		t.Fatal("future head erased the original epoch mapping", mapping, err)
	}
	if err := protocol.VerifyProspectiveWalletMapping(t.Context(), mapping, fixture.config.ClientKeyRootSigner, request.Authority.Start.Number, fixture.input.Clock.StartTime.Unix()); err != nil {
		t.Fatal("future rotation changed earlier prospective eligibility", err)
	}
}

// Unknown epoch eligibility never removes a provider from the complete roster.
func TestPrepareRetainsExpiredAndFutureConsentProviders(t *testing.T) {
	for _, future := range []bool{false, true} {
		fixture := newRosterProducerTestFixture(t)
		statement := rosterProducerTestStatement(fixture)
		if future {
			statement.FromEpoch = fixture.input.Epoch + 1
		} else {
			statement.ThroughEpoch = fixture.input.Epoch - 1
		}
		original, hash := rosterProducerTestConsent(t, &statement, 93, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{94}})
		fixture.input.Providers[0].WalletConsents = []protocol.WalletMappingConsent{original}
		_, request := rosterProducerTestPrepare(t, fixture)
		if len(request.Authority.ExpectedProviders) != 1 {
			t.Fatal("unavailable mapping removed an expected provider", future)
		}
		provider := request.Authority.ExpectedProviders[0]
		if provider.ClientId != statement.ClientId || provider.NetworkId != statement.NetworkId || provider.WalletHeadHash != hex.EncodeToString(hash[:]) || provider.WalletGeneration != 1 {
			t.Fatal("unavailable mapping lost its explicit retained head", future, provider)
		}
		mapping, err := protocol.VerifyWalletMappingHistory(t.Context(), request.Input.Providers[0].WalletConsents, protocol.WalletMappingHistoryExpectation{
			Domain: request.Authority.Domain, ClientId: provider.ClientId, HeadHash: hash, Generation: provider.WalletGeneration, Epoch: request.Authority.Epoch,
		})
		if mapping != nil || !errors.Is(err, protocol.ErrWalletMappingUnavailable) {
			t.Fatal("expired or future consent became known at this epoch", future, mapping, err)
		}
	}
}

// Lack of any wallet original is explicit unknown mapping, not absent work.
func TestPrepareRetainsProviderWithoutWalletConsent(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	_, request := rosterProducerTestPrepare(t, fixture)
	if len(request.Authority.ExpectedProviders) != 1 || request.Authority.ExpectedProviders[0].ClientId != fixture.input.Providers[0].ClientId || request.Authority.ExpectedProviders[0].WalletHeadHash != "" || request.Authority.ExpectedProviders[0].WalletGeneration != 0 {
		t.Fatal("provider without consent was omitted or assigned a wallet", request.Authority.ExpectedProviders)
	}
}

// Wallet and operator signatures for another network cannot borrow this owner.
func TestPrepareRefusesSignedWalletNetworkMismatch(t *testing.T) {
	fixture := newRosterProducerTestFixture(t)
	statement := rosterProducerTestStatement(fixture)
	statement.NetworkId[0]++
	original, _ := rosterProducerTestConsent(t, &statement, 93, fixture.registrationKey, protocol.ClientKeyEffectiveBoundary{Epoch: 7, Block: 98, Hash: [32]byte{94}})
	fixture.input.Providers[0].WalletConsents = []protocol.WalletMappingConsent{original}
	if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, protocol.ErrWalletMappingIntegrity) || len(prepared) != 0 {
		t.Fatal("foreign signed wallet network gained provider authority", err)
	}
}

// The selected mapping and final head stay valid while a test changes one
// unselected original. Every successor still signs the exact predecessor hash.
func rosterProducerTestHistory(t *testing.T, fixture *rosterProducerTestFixture, mutate func(int, *protocol.WalletMappingStatement) *ecdsa.PrivateKey) []protocol.WalletMappingConsent {
	t.Helper()
	originals := []protocol.WalletMappingConsent{}
	var previousHash [32]byte
	for index, fromEpoch := range []uint64{7, 8, 10, 11} {
		statement := rosterProducerTestStatement(fixture)
		statement.Generation, statement.PreviousHash, statement.FromEpoch = uint64(index+1), previousHash, fromEpoch
		statement.Nonce[0] += byte(index)
		boundary := protocol.ClientKeyEffectiveBoundary{Epoch: fromEpoch - 1, Block: 98, Hash: [32]byte{byte(101 + index)}}
		if fromEpoch > fixture.input.Epoch {
			boundary.Block = uint64(150 + index)
			statement.IssuedAt, statement.ExpiresAt = fixture.input.Clock.StartTime.Unix()+600, fixture.input.Clock.StartTime.Unix()+900
		}
		root := fixture.registrationKey
		if mutate != nil {
			root = mutate(index, &statement)
		}
		original, hash := rosterProducerTestConsent(t, &statement, byte(93+index), root, boundary)
		originals = append(originals, original)
		previousHash = hash
	}
	return originals
}

// Checking only the effective mapping and final head misses contradictions in
// either an older original or an intermediate future generation.
func TestPrepareRefusesUnselectedWalletNetworkMismatch(t *testing.T) {
	for _, changedIndex := range []int{0, 2} {
		fixture := newRosterProducerTestFixture(t)
		fixture.input.Providers[0].WalletConsents = rosterProducerTestHistory(t, fixture, nil)
		rosterProducerTestPrepare(t, fixture)
		fixture.input.Providers[0].WalletConsents = rosterProducerTestHistory(t, fixture, func(index int, statement *protocol.WalletMappingStatement) *ecdsa.PrivateKey {
			if index == changedIndex {
				statement.NetworkId[0]++
			}
			return fixture.registrationKey
		})
		if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, protocol.ErrWalletMappingIntegrity) || len(prepared) != 0 {
			t.Fatal("unselected consent borrowed another network", changedIndex, err)
		}
	}
}

// A valid foreign operator signature cannot hide in unselected consent history.
func TestPrepareRefusesUnselectedWalletRootMismatch(t *testing.T) {
	for _, changedIndex := range []int{0, 2} {
		fixture := newRosterProducerTestFixture(t)
		fixture.input.Providers[0].WalletConsents = rosterProducerTestHistory(t, fixture, nil)
		rosterProducerTestPrepare(t, fixture)
		foreignKey := rosterProducerTestKey(t, 52)
		fixture.input.Providers[0].WalletConsents = rosterProducerTestHistory(t, fixture, func(index int, _ *protocol.WalletMappingStatement) *ecdsa.PrivateKey {
			if index == changedIndex {
				return foreignKey
			}
			return fixture.registrationKey
		})
		if prepared, err := Prepare(t.Context(), fixture.config, rosterProducerTestJson(t, fixture.input)); !errors.Is(err, protocol.ErrWalletMappingIntegrity) || len(prepared) != 0 {
			t.Fatal("unselected consent borrowed another operator root", changedIndex, err)
		}
	}
}
