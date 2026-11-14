// Still-open originals bind a physical observation and the exact requested
// boundary while preserving previously retained receipt bytes unchanged.
package protocol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"testing"
)

// This original is observed after its exact boundary under a distinct admitted
// source key; the receipt itself cannot choose the caller's source authority.
func providerWorkOpenTestOriginal() (ProviderWorkReceipt, ProviderWorkSourceAuthority, ed25519.PrivateKey) {
	authority, key := providerWorkTestAuthority()
	value := ProviderWorkReceipt{DomainHash: authority.DomainHash, SourceId: authority.SourceId, Generation: authority.Generation,
		Open: &ProviderWorkOpenObservation{ContractId: "00000000-0000-0000-0000-000000000003", ReservationHash: [32]byte{4}, Epoch: 0, Block: 101, BlockHash: [32]byte{5}, BoundaryUnixMicro: 1200, ObservedAtUnixMicro: 1300}}
	return value, authority, key
}

// Changing any selected boundary or original reservation invalidates the actual
// signature; a source outside its independently selected interval stays unknown.
func TestProviderWorkOpenObservationBindsBoundaryAndIndependentSource(t *testing.T) {
	value, authority, key := providerWorkOpenTestOriginal()
	original, err := SignProviderWorkReceipt(t.Context(), value, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := original.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeProviderWorkReceipt(t.Context(), raw)
	if err != nil || decoded.Open == nil || *decoded.Open != *value.Open || decoded.ObservedUnixMicro() != value.Open.ObservedAtUnixMicro {
		t.Fatal("original still-open boundary did not survive canonical transport", decoded, err)
	}
	if err := VerifyProviderWorkReceiptAuthority(t.Context(), decoded, authority); err != nil {
		t.Fatal("independent source did not authorize its original observation", err)
	}
	for _, mutate := range []func(*ProviderWorkOpenObservation){
		func(v *ProviderWorkOpenObservation) { v.ContractId = "00000000-0000-0000-0000-000000000009" },
		func(v *ProviderWorkOpenObservation) { v.ReservationHash[0]++ },
		func(v *ProviderWorkOpenObservation) { v.Epoch++ },
		func(v *ProviderWorkOpenObservation) { v.Block++ },
		func(v *ProviderWorkOpenObservation) { v.BlockHash[0]++ },
		func(v *ProviderWorkOpenObservation) { v.BoundaryUnixMicro++ },
		func(v *ProviderWorkOpenObservation) { v.ObservedAtUnixMicro++ },
	} {
		changed := original
		body := *original.Open
		changed.Open = &body
		mutate(changed.Open)
		if err := changed.Verify(t.Context()); !errors.Is(err, ErrProviderWorkIntegrity) {
			t.Fatal("changed open observation borrowed an original signature", err)
		}
	}
	for _, mutate := range []func(*ProviderWorkSourceAuthority){
		func(v *ProviderWorkSourceAuthority) { v.DomainHash[0]++ },
		func(v *ProviderWorkSourceAuthority) { v.Generation = "00000000-0000-0000-0000-000000000009" },
		func(v *ProviderWorkSourceAuthority) { v.PublicKey[0]++ },
	} {
		foreign := authority
		mutate(&foreign)
		if err := VerifyProviderWorkReceiptAuthority(t.Context(), original, foreign); !errors.Is(err, ErrProviderWorkIntegrity) {
			t.Fatal("open observation selected its own independent source", err)
		}
	}
	authority.ThroughUnixMicro = original.Open.ObservedAtUnixMicro
	if err := VerifyProviderWorkReceiptAuthority(t.Context(), original, authority); !errors.Is(err, ErrProviderWorkUnavailable) {
		t.Fatal("open observation escaped the source's exclusive validity interval", err)
	}
}

// Invalid physical clocks cannot leave the producer as signed originals.
// Full unsigned boundary identities remain exact, and cancellation publishes none.
func TestProviderWorkOpenObservationRefusesInvalidClockAndMixedBodies(t *testing.T) {
	for _, mutate := range []func(*ProviderWorkReceipt){
		func(v *ProviderWorkReceipt) { v.Open.ReservationHash = [32]byte{} },
		func(v *ProviderWorkReceipt) { v.Open.Block = 0 },
		func(v *ProviderWorkReceipt) { v.Open.BlockHash = [32]byte{} },
		func(v *ProviderWorkReceipt) { v.Open.BoundaryUnixMicro = 0 },
		func(v *ProviderWorkReceipt) { v.Open.ObservedAtUnixMicro = v.Open.BoundaryUnixMicro - 1 },
		func(v *ProviderWorkReceipt) {
			v.Outcome = &ProviderWorkOutcome{ContractId: v.Open.ContractId, ReservationHash: v.Open.ReservationHash, ClosedAtUnixMicro: v.Open.ObservedAtUnixMicro, Outcome: "settled"}
		},
	} {
		value, _, key := providerWorkOpenTestOriginal()
		mutate(&value)
		if _, err := SignProviderWorkReceipt(t.Context(), value, key); !errors.Is(err, ErrProviderWorkIntegrity) {
			t.Fatal("invalid still-open original was signed", err)
		}
	}
	value, _, key := providerWorkOpenTestOriginal()
	value.Open.ObservedAtUnixMicro = value.Open.BoundaryUnixMicro
	if _, err := SignProviderWorkReceipt(t.Context(), value, key); err != nil {
		t.Fatal("exact boundary observation was rejected", err)
	}
	value.Open.Epoch, value.Open.Block = ^uint64(0), ^uint64(0)
	if result, err := SignProviderWorkReceipt(t.Context(), value, key); err != nil || result.Open == nil || result.Open.Epoch != value.Open.Epoch || result.Open.Block != value.Open.Block {
		t.Fatal("original unsigned boundary identity was narrowed", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := SignProviderWorkReceipt(ctx, value, key); !errors.Is(err, context.Canceled) || result.Open != nil {
		t.Fatal("canceled owner published an open observation", result, err)
	}
}

// This frozen pre-extension wire shape independently signs a legacy original.
// The new optional body must not change canonical bytes for any old receipt.
func TestProviderWorkOpenExtensionPreservesOriginalReceiptGrammar(t *testing.T) {
	authority, key := providerWorkTestAuthority()
	baseline := providerWorkTestEvent(t, authority, key, "baseline", nil, nil)
	legacy := struct {
		Schema      string                      `json:"schema"`
		DomainHash  [32]byte                    `json:"domain_hash"`
		SourceId    string                      `json:"source_id"`
		Generation  string                      `json:"generation"`
		PublicKey   [32]byte                    `json:"public_key"`
		Session     *ProviderWorkSessionEvent   `json:"session,omitempty"`
		Reservation *ProviderWorkReservation    `json:"reservation,omitempty"`
		Stream      *ProviderWorkStreamCohort   `json:"stream,omitempty"`
		Outcome     *ProviderWorkOutcome        `json:"outcome,omitempty"`
		Signature   [ed25519.SignatureSize]byte `json:"signature"`
	}{Schema: baseline.Schema, DomainHash: baseline.DomainHash, SourceId: baseline.SourceId, Generation: baseline.Generation, PublicKey: baseline.PublicKey, Session: baseline.Session}
	unsigned, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	copy(legacy.Signature[:], ed25519.Sign(key, unsigned))
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	original, err := DecodeProviderWorkReceipt(t.Context(), raw)
	if err != nil || original.Open != nil {
		t.Fatal("new open body invalidated a retained legacy original", err)
	}
	again, err := original.Bytes(t.Context())
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("retained legacy bytes were rewritten by optional open body", err)
	}
}
