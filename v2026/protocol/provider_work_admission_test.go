package protocol

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
)

func providerWorkTestAuthority() (ProviderWorkSourceAuthority, ed25519.PrivateKey) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	value := ProviderWorkSourceAuthority{DomainHash: [32]byte{8}, SourceId: "00000000-0000-0000-0000-000000000001", Generation: "00000000-0000-0000-0000-000000000002", FromUnixMicro: 1000, ThroughUnixMicro: 2000, MaxEndpointEvents: 16, MaxCohortMembers: 4, DirectoryPublicKeys: [][32]byte{}}
	copy(value.PublicKey[:], key.Public().(ed25519.PublicKey))
	return value, key
}

func providerWorkTestEvent(t *testing.T, authority ProviderWorkSourceAuthority, key ed25519.PrivateKey, kind string, previous *ProviderWorkReceipt, extender *ProviderWorkExtender) ProviderWorkReceipt {
	t.Helper()
	event := &ProviderWorkSessionEvent{ClientId: "00000000-0000-0000-0000-000000000003", NetworkId: "00000000-0000-0000-0000-000000000004", Sequence: 1, Kind: kind, ObservedAtUnixMicro: 1000, Extender: extender}
	if previous != nil {
		event.Sequence = previous.Session.Sequence + 1
		event.ObservedAtUnixMicro += int64(event.Sequence)
		var err error
		event.PreviousHash, err = previous.ContentHash(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		event.ConnectionId = "00000000-0000-0000-0000-000000000005"
	}
	original, err := SignProviderWorkReceipt(t.Context(), ProviderWorkReceipt{DomainHash: authority.DomainHash, SourceId: authority.SourceId, Generation: authority.Generation, Session: event}, key)
	if err != nil {
		t.Fatal(err)
	}
	return original
}

func TestProviderWorkReplayProvesOriginalEmptyThenRetiredExtender(t *testing.T) {
	authority, key := providerWorkTestAuthority()
	baseline := providerWorkTestEvent(t, authority, key, "baseline", nil, nil)
	extender := &ProviderWorkExtender{ExtenderId: "00000000-0000-0000-0000-000000000006", ClientId: "00000000-0000-0000-0000-000000000007", NetworkId: "00000000-0000-0000-0000-000000000008"}
	admit := providerWorkTestEvent(t, authority, key, "admit", &baseline, extender)
	retire := providerWorkTestEvent(t, authority, key, "retire", &admit, nil)
	states, err := ReplayProviderWorkEndpoint(t.Context(), authority, []ProviderWorkReceipt{baseline, admit, retire})
	if err != nil || len(states) != 3 || states[0].ActiveExtenders != 0 || states[1].ActiveExtenders != 1 || states[2].ActiveExtenders != 0 || states[2].ActiveConnections != 0 {
		t.Fatalf("original connection lifecycle = %+v, %v", states, err)
	}
}

func TestProviderWorkReplayDroppedAdmissionCannotBecomeEmpty(t *testing.T) {
	authority, key := providerWorkTestAuthority()
	baseline := providerWorkTestEvent(t, authority, key, "baseline", nil, nil)
	admit := providerWorkTestEvent(t, authority, key, "admit", &baseline, nil)
	retire := providerWorkTestEvent(t, authority, key, "retire", &admit, nil)
	states, err := ReplayProviderWorkEndpoint(t.Context(), authority, []ProviderWorkReceipt{baseline, retire})
	if !errors.Is(err, ErrProviderWorkUnavailable) || len(states) != 1 || states[0].Head.Sequence != 1 {
		t.Fatalf("missing original was healed: %+v, %v", states, err)
	}
}

func TestProviderWorkReplayRejectsRetirementWithoutOriginalConnection(t *testing.T) {
	authority, key := providerWorkTestAuthority()
	baseline := providerWorkTestEvent(t, authority, key, "baseline", nil, nil)
	retire := providerWorkTestEvent(t, authority, key, "retire", &baseline, nil)
	if _, err := ReplayProviderWorkEndpoint(t.Context(), authority, []ProviderWorkReceipt{baseline, retire}); !errors.Is(err, ErrProviderWorkIntegrity) {
		t.Fatalf("unowned retirement accepted: %v", err)
	}
}

func TestProviderWorkOriginalCannotSelectForeignDomainOrSourceGeneration(t *testing.T) {
	authority, key := providerWorkTestAuthority()
	original := providerWorkTestEvent(t, authority, key, "baseline", nil, nil)
	for _, mutate := range []func(*ProviderWorkSourceAuthority){
		func(value *ProviderWorkSourceAuthority) { value.DomainHash[0]++ },
		func(value *ProviderWorkSourceAuthority) { value.Generation = "00000000-0000-0000-0000-000000000009" },
		func(value *ProviderWorkSourceAuthority) { value.PublicKey[0]++ },
	} {
		foreign := authority
		mutate(&foreign)
		if err := VerifyProviderWorkReceiptAuthority(t.Context(), original, foreign); !errors.Is(err, ErrProviderWorkIntegrity) {
			t.Fatalf("foreign authority admitted: %v", err)
		}
	}
}

func TestProviderWorkOriginalRejectsUnsignedParticipantMutationAndJsonAlias(t *testing.T) {
	authority, key := providerWorkTestAuthority()
	original := providerWorkTestEvent(t, authority, key, "baseline", nil, nil)
	raw, err := original.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeProviderWorkReceipt(t.Context(), append(raw, '\n')); !errors.Is(err, ErrProviderWorkIntegrity) {
		t.Fatalf("noncanonical original accepted: %v", err)
	}
	original.Session.NetworkId = "00000000-0000-0000-0000-000000000009"
	if err := original.Verify(t.Context()); !errors.Is(err, ErrProviderWorkIntegrity) {
		t.Fatalf("mutated original party accepted: %v", err)
	}
}

func TestProviderWorkReplayPreservesCancellationAndFiniteAdmission(t *testing.T) {
	authority, key := providerWorkTestAuthority()
	baseline := providerWorkTestEvent(t, authority, key, "baseline", nil, nil)
	admit := providerWorkTestEvent(t, authority, key, "admit", &baseline, nil)
	authority.MaxEndpointEvents = 1
	if _, err := ReplayProviderWorkEndpoint(t.Context(), authority, []ProviderWorkReceipt{baseline, admit}); !errors.Is(err, ErrProviderWorkCapacity) {
		t.Fatalf("authority replay limit ignored: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ReplayProviderWorkEndpoint(ctx, authority, []ProviderWorkReceipt{baseline}); !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped owner replayed: %v", err)
	}
}

func TestProviderWorkPresenceOnlyExtenderRetirementRestoresProvenAbsence(t *testing.T) {
	authority, key := providerWorkTestAuthority()
	baseline := providerWorkTestEvent(t, authority, key, "baseline", nil, nil)
	admit := providerWorkTestEvent(t, authority, key, "admit", &baseline, nil)
	admit.Session.ExtenderId = "00000000-0000-0000-0000-000000000006"
	admit, err := SignProviderWorkReceipt(t.Context(), admit, key)
	if err != nil {
		t.Fatal(err)
	}
	retire := providerWorkTestEvent(t, authority, key, "retire", &admit, nil)
	states, err := ReplayProviderWorkEndpoint(t.Context(), authority, []ProviderWorkReceipt{baseline, admit, retire})
	if err != nil || len(states) != 3 || states[1].ActiveExtenders != 1 || states[2].ActiveExtenders != 0 || states[2].ActiveConnections != 0 {
		t.Fatalf("presence-only original left a permanent gap after retirement: %+v, %v", states, err)
	}
}

func TestProviderWorkAuthorityThroughBoundaryIsExclusive(t *testing.T) {
	authority, key := providerWorkTestAuthority()
	authority.ThroughUnixMicro = authority.FromUnixMicro + 1
	original := providerWorkTestEvent(t, authority, key, "baseline", nil, nil)
	if err := VerifyProviderWorkReceiptAuthority(t.Context(), original, authority); err != nil {
		t.Fatalf("inclusive start refused: %v", err)
	}
	original.Session.ObservedAtUnixMicro = authority.ThroughUnixMicro
	original, err := SignProviderWorkReceipt(t.Context(), original, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProviderWorkReceiptAuthority(t.Context(), original, authority); !errors.Is(err, ErrProviderWorkUnavailable) {
		t.Fatalf("exclusive through admitted: %v", err)
	}
	authority.ThroughUnixMicro = authority.FromUnixMicro
	if err := authority.Validate(); !errors.Is(err, ErrProviderWorkIntegrity) {
		t.Fatalf("empty source validity interval admitted: %v", err)
	}
}
