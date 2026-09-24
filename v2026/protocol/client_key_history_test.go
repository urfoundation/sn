// These controls use real independent operator signatures. No validation
// callback or current API key is substituted for a historical statement.
package protocol

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Fixed test keys make both the original signed record and each mutation
// reproducible; production keys are obtained only by the server owner.
func newClientKeyHistoryTestRegistration(t *testing.T) (ClientKeyRegistration, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("12", 32))
	if err != nil {
		t.Fatal(err)
	}
	publicKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	registration := ClientKeyRegistration{
		Domain:   ClientKeyHistoryDomain{ChainID: 42, GenesisHash: [32]byte{1}, Netuid: 521, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 6},
		ClientID: [16]byte{7}, NetworkID: [16]byte{8}, Generation: 1, Present: true,
		PublicKey: [32]byte(publicKey), EffectiveBoundary: ClientKeyEffectiveBoundary{Epoch: 0, Block: 70, Hash: [32]byte{9}},
	}
	if err := SignClientKeyRegistration(&registration, key); err != nil {
		t.Fatal(err)
	}
	return registration, key
}

// Returns one response whose exact request is known independently of the wire.
func newClientKeyHistoryTestObservation(t *testing.T, registration ClientKeyRegistration, key *ecdsa.PrivateKey) ClientKeyObservation {
	t.Helper()
	registrationHash, err := registration.ContentHash()
	if err != nil {
		t.Fatal(err)
	}
	observation := ClientKeyObservation{
		Domain: registration.Domain, ClientID: registration.ClientID, Generation: registration.Generation, RegistrationHash: registrationHash,
		Request: ClientKeyObservationRequest{ClientID: registration.ClientID, ValidatorHotkey: [32]byte{11}, NativeBlock: 80, NativeHash: [32]byte{12}, NativeEpoch: 3, DecisionBoundary: ClientKeyEffectiveBoundary{Epoch: 1, Block: 79, Hash: [32]byte{13}}, Nonce: [32]byte{14}},
	}
	if err := SignClientKeyObservation(&observation, key); err != nil {
		t.Fatal(err)
	}
	return observation
}

// First registration and epoch zero do not depend on validator activation or
// a terminal census. Both complete byte identities round-trip exactly.
func TestClientKeyHistoryFirstRegistrationAndObservationRoundTrip(t *testing.T) {
	t.Parallel()
	registration, key := newClientKeyHistoryTestRegistration(t)
	if err := registration.Follows(nil); err != nil {
		t.Fatal(err)
	}
	encoded, err := registration.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeClientKeyRegistration(encoded)
	if err != nil || decoded != registration {
		t.Fatalf("registration round trip differs: %+v, %v", decoded, err)
	}
	contentHash, err := registration.ContentHash()
	signingHash, signingErr := registration.Digest()
	if err != nil || signingErr != nil || contentHash != sha256.Sum256(encoded) || contentHash == signingHash {
		t.Fatalf("full-byte and signing identities were conflated: %x, %x, %v, %v", contentHash, signingHash, err, signingErr)
	}
	observation := newClientKeyHistoryTestObservation(t, registration, key)
	response, err := observation.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	decodedObservation, err := DecodeClientKeyObservation(response)
	if err != nil || decodedObservation != observation {
		t.Fatalf("observation round trip differs: %+v, %v", decodedObservation, err)
	}
	if err := observation.VerifyRegistration(registration, registration.Domain, observation.Request, registration.Signer, observation.Signer); err != nil {
		t.Fatal(err)
	}
}

// Each transition retains the actual old signed bytes through rotation,
// clearing and re-registration; an empty current key cannot erase history.
func TestClientKeyHistoryRotationAndDeletionPreserveOriginalBytes(t *testing.T) {
	t.Parallel()
	initial, key := newClientKeyHistoryTestRegistration(t)
	initialBytes, _ := initial.Bytes()
	prior := initial
	for index, nextKey := range [][32]byte{{21}, {}, {23}} {
		next := prior
		next.Generation++
		next.PreviousHash, _ = prior.ContentHash()
		next.PublicKey, next.Present = nextKey, nextKey != ([32]byte{})
		next.EffectiveBoundary.Block++
		next.EffectiveBoundary.Hash = [32]byte{byte(31 + index)}
		if err := SignClientKeyRegistration(&next, key); err != nil {
			t.Fatal(err)
		}
		if err := next.Follows(&prior); err != nil {
			t.Fatalf("transition %d rejected: %v", index, err)
		}
		prior = next
	}
	currentBytes, _ := initial.Bytes()
	if !bytes.Equal(initialBytes, currentBytes) || prior.Generation != 4 {
		t.Fatal("rotation or deletion rewrote the historical key statement")
	}
	if _, err := DecodeClientKeyRegistration(initialBytes); err != nil {
		t.Fatalf("deleted original statement is no longer independently verifiable: %v", err)
	}
}

// Genuine signatures on a different branch still fail history continuity.
func TestClientKeyHistoryRejectsSkippedOrSubstitutedPredecessor(t *testing.T) {
	t.Parallel()
	prior, key := newClientKeyHistoryTestRegistration(t)
	valid := prior
	valid.Generation = 2
	valid.PublicKey = [32]byte{19}
	valid.PreviousHash, _ = prior.ContentHash()
	for _, mutate := range []func(*ClientKeyRegistration){
		func(value *ClientKeyRegistration) { value.Generation++ },
		func(value *ClientKeyRegistration) { value.PreviousHash[1]++ },
		func(value *ClientKeyRegistration) { value.ClientID[1]++ },
		func(value *ClientKeyRegistration) { value.NetworkID[1]++ },
		func(value *ClientKeyRegistration) { value.Domain.NoID++ },
		func(value *ClientKeyRegistration) { value.PublicKey = prior.PublicKey },
	} {
		candidate := valid
		mutate(&candidate)
		if err := SignClientKeyRegistration(&candidate, key); err != nil {
			t.Fatal(err)
		}
		if err := candidate.Follows(&prior); err == nil {
			t.Fatalf("signed noncontiguous history accepted: %+v", candidate)
		}
	}
}

// A repeated height requires the identical epoch/hash; a later generation
// cannot choose an older finalized identity even under the actual signer.
func TestClientKeyHistoryRejectsEffectiveBoundaryRollback(t *testing.T) {
	t.Parallel()
	prior, key := newClientKeyHistoryTestRegistration(t)
	prior.EffectiveBoundary.Epoch = 2
	if err := SignClientKeyRegistration(&prior, key); err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []ClientKeyEffectiveBoundary{
		{Epoch: 1, Block: 71, Hash: [32]byte{10}},
		{Epoch: 2, Block: 69, Hash: [32]byte{10}},
		{Epoch: 2, Block: 70, Hash: [32]byte{10}},
		{Epoch: 3, Block: 70, Hash: prior.EffectiveBoundary.Hash},
	} {
		next := prior
		next.Generation = 2
		next.PublicKey = [32]byte{20}
		next.PreviousHash, _ = prior.ContentHash()
		next.EffectiveBoundary = boundary
		if err := SignClientKeyRegistration(&next, key); err != nil {
			t.Fatal(err)
		}
		if err := next.Follows(&prior); err == nil {
			t.Fatalf("signed boundary rollback accepted: %+v", boundary)
		}
	}
}

// The old registration remains authentic after rotation, but its old signed
// API response cannot answer the new request, even at the same decision cut.
func TestClientKeyHistoryOldObservationCannotAnswerFreshRequest(t *testing.T) {
	t.Parallel()
	registration, key := newClientKeyHistoryTestRegistration(t)
	observation := newClientKeyHistoryTestObservation(t, registration, key)
	request := observation.Request
	request.Nonce[1]++
	if err := observation.VerifyRegistration(registration, registration.Domain, request, registration.Signer, observation.Signer); err == nil {
		t.Fatal("historical API response answered a fresh observation request")
	}
	rotated := registration
	rotated.Generation = 2
	rotated.PreviousHash, _ = registration.ContentHash()
	rotated.PublicKey = [32]byte{22}
	if err := SignClientKeyRegistration(&rotated, key); err != nil {
		t.Fatal(err)
	}
	if err := observation.VerifyRegistration(rotated, rotated.Domain, observation.Request, rotated.Signer, observation.Signer); err == nil {
		t.Fatal("old observation authenticated a different current generation")
	}
}

// Public signatures do not replace independently read domain and signer pins.
func TestClientKeyHistoryObservationRequiresIndependentDomainAndSigners(t *testing.T) {
	t.Parallel()
	registration, key := newClientKeyHistoryTestRegistration(t)
	observation := newClientKeyHistoryTestObservation(t, registration, key)
	for _, mutate := range []func(*ClientKeyHistoryDomain){
		func(value *ClientKeyHistoryDomain) { value.ChainID++ },
		func(value *ClientKeyHistoryDomain) { value.GenesisHash[1]++ },
		func(value *ClientKeyHistoryDomain) { value.Netuid++ },
		func(value *ClientKeyHistoryDomain) { value.Coordinator[1]++ },
		func(value *ClientKeyHistoryDomain) { value.SettlementVault[1]++ },
		func(value *ClientKeyHistoryDomain) { value.DeploymentIDHash[1]++ },
		func(value *ClientKeyHistoryDomain) { value.PolicyHash[1]++ },
		func(value *ClientKeyHistoryDomain) { value.NoID++ },
	} {
		domain := registration.Domain
		mutate(&domain)
		if err := observation.VerifyRegistration(registration, domain, observation.Request, registration.Signer, observation.Signer); err == nil {
			t.Fatalf("substituted domain accepted: %+v", domain)
		}
	}
	if err := observation.VerifyRegistration(registration, registration.Domain, observation.Request, common.Address{66}, observation.Signer); err == nil {
		t.Fatal("unapproved registration root signer accepted")
	}
	if err := observation.VerifyRegistration(registration, registration.Domain, observation.Request, registration.Signer, common.Address{67}); err == nil {
		t.Fatal("unapproved observation root signer accepted")
	}
}

// All request routing fields participate, not merely the API response's key.
func TestClientKeyHistoryObservationRejectsRequestAndSignatureTamper(t *testing.T) {
	t.Parallel()
	registration, key := newClientKeyHistoryTestRegistration(t)
	observation := newClientKeyHistoryTestObservation(t, registration, key)
	for _, mutate := range []func(*ClientKeyObservationRequest){
		func(value *ClientKeyObservationRequest) { value.ClientID[1]++ },
		func(value *ClientKeyObservationRequest) { value.ValidatorHotkey[1]++ },
		func(value *ClientKeyObservationRequest) { value.NativeBlock++ },
		func(value *ClientKeyObservationRequest) { value.NativeHash[1]++ },
		func(value *ClientKeyObservationRequest) { value.NativeEpoch++ },
		func(value *ClientKeyObservationRequest) { value.DecisionBoundary.Epoch++ },
		func(value *ClientKeyObservationRequest) { value.DecisionBoundary.Block++ },
		func(value *ClientKeyObservationRequest) { value.DecisionBoundary.Hash[1]++ },
		func(value *ClientKeyObservationRequest) { value.Nonce[1]++ },
	} {
		candidate := observation
		mutate(&candidate.Request)
		if err := candidate.VerifySignature(); err == nil {
			t.Fatal("modified API request retained a valid signature")
		}
	}
	observation.Signature[12] ^= 1
	if err := observation.VerifySignature(); err == nil {
		t.Fatal("tampered operator observation signature accepted")
	}
}

// JSON custody is exact, not a permissive unmarshal followed by re-signing.
func TestClientKeyHistoryCanonicalDecodersRejectAliasesAndTrailingBytes(t *testing.T) {
	t.Parallel()
	registration, key := newClientKeyHistoryTestRegistration(t)
	observation := newClientKeyHistoryTestObservation(t, registration, key)
	for _, original := range []any{registration, observation} {
		encoded, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range [][]byte{
			append(bytes.Clone(encoded), '\n'),
			append(bytes.Clone(encoded), []byte("{}")...),
			append([]byte(`{"unknown":1,`), encoded[1:]...),
			append([]byte(`{"schema":"discarded",`), encoded[1:]...),
			encoded[:len(encoded)-1],
			bytes.Repeat([]byte{' '}, MaxClientKeyStatementBytes+1),
		} {
			switch original.(type) {
			case ClientKeyRegistration:
				decoded, err := DecodeClientKeyRegistration(candidate)
				if err == nil || decoded != (ClientKeyRegistration{}) {
					t.Fatal("noncanonical registration returned usable authority")
				}
			case ClientKeyObservation:
				decoded, err := DecodeClientKeyObservation(candidate)
				if err == nil || decoded != (ClientKeyObservation{}) {
					t.Fatal("noncanonical observation returned usable authority")
				}
			}
		}
	}
}

// Malformed local owners fail without mutating the previously signed objects.
func TestClientKeyHistoryInvalidSigningOwnerLeavesBothStatementsUnchanged(t *testing.T) {
	t.Parallel()
	registration, key := newClientKeyHistoryTestRegistration(t)
	observation := newClientKeyHistoryTestObservation(t, registration, key)
	wrongPublic := *key
	wrongPublic.D = big.NewInt(1)
	for _, candidate := range []*ecdsa.PrivateKey{nil, {}, {D: big.NewInt(1)}, &wrongPublic} {
		nextRegistration, nextObservation := registration, observation
		if err := SignClientKeyRegistration(&nextRegistration, candidate); err == nil || nextRegistration != registration {
			t.Fatal("invalid local owner changed the registration")
		}
		if err := SignClientKeyObservation(&nextObservation, candidate); err == nil || nextObservation != observation {
			t.Fatal("invalid local owner changed the observation")
		}
	}
}

// Malleable high-s and non-recoverable variants cannot acquire another content
// identity for the same operator statement or poison a predecessor chain.
func TestClientKeyHistoryRejectsMalleableSignatureAndInvalidKeyPresence(t *testing.T) {
	t.Parallel()
	registration, key := newClientKeyHistoryTestRegistration(t)
	highS := registration
	value := new(big.Int).SetBytes(highS.Signature[32:64])
	value.Sub(crypto.S256().Params().N, value).FillBytes(highS.Signature[32:64])
	highS.Signature[64] ^= 1
	if err := highS.VerifySignature(); err == nil {
		t.Fatal("malleable high-s registration accepted")
	}
	for _, mutate := range []func(*ClientKeyRegistration){
		func(value *ClientKeyRegistration) { value.PublicKey = [32]byte{} },
		func(value *ClientKeyRegistration) { value.Present = false },
		func(value *ClientKeyRegistration) { value.Generation = 0 },
		func(value *ClientKeyRegistration) { value.PreviousHash[1]++ },
	} {
		candidate := registration
		mutate(&candidate)
		if err := SignClientKeyRegistration(&candidate, key); err == nil {
			t.Fatal("invalid key/generation acquired an operator signature")
		}
	}
	registration.Signature[64] = 27
	if err := registration.VerifySignature(); err == nil {
		t.Fatal("noncanonical recovery byte accepted")
	}
}
