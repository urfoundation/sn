// Offline profile admission keeps the exact independently reviewed scope while
// the original outbox is absent or held by a separate restore descriptor.
package miner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The two synthetic owners deliberately have no physical outboxes yet.
func portableProviderWorkCaptureFixture(t *testing.T) ProviderWorkCaptureProfile {
	t.Helper()
	directory := t.TempDir()
	return ProviderWorkCaptureProfile{
		Schema: ProviderWorkCaptureSchema, ApiUrl: "https://operator.example", RequestPublicKey: [32]byte{101},
		Providers: []ProviderWorkCaptureOwner{
			{Slot: "direct", ClientId: [16]byte{102}, PublicKey: [32]byte{103}, Domain: providerCloseDomainFixture(), OutboxDirectory: filepath.Join(directory, "original-direct")},
			{Slot: "proxy-0", ClientId: [16]byte{104}, PublicKey: [32]byte{105}, Domain: providerCloseDomainFixture(), OutboxDirectory: filepath.Join(directory, "original-proxy")},
		},
	}
}

// Hash exact supplied bytes, including whitespace and any rejected grammar.
func portableProviderWorkCaptureDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// A fresh target and a moved original both retain the complete original public
// scope. Neither offline decode nor failed launch admission creates custody.
func TestProviderWholeWorkPortableProfilePreservesOriginalScopeWithoutLiveOutbox(t *testing.T) {
	profile := portableProviderWorkCaptureFixture(t)
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	digest := portableProviderWorkCaptureDigest(raw)
	for _, moved := range []bool{false, true} {
		if moved {
			for _, provider := range profile.Providers {
				if err := os.Mkdir(provider.OutboxDirectory, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(provider.OutboxDirectory, provider.OutboxDirectory+"-retained"); err != nil {
					t.Fatal(err)
				}
			}
		}
		loaded, err := DecodeProviderWorkCaptureProfile(t.Context(), raw, digest)
		if err != nil || loaded == nil || !reflect.DeepEqual(*loaded, profile) {
			t.Fatal("offline admission changed exact approved roster or authority", moved, loaded, err)
		}
		if err := loaded.Validate(); err == nil {
			t.Fatal("offline scope was mistaken for live original custody", moved)
		}
		for _, provider := range profile.Providers {
			if _, err := os.Lstat(provider.OutboxDirectory); !os.IsNotExist(err) {
				t.Fatal("profile admission recreated the absent original outbox", moved, err)
			}
		}
	}
	path := filepath.Join(t.TempDir(), "reviewed-profile.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if loaded, err := ReadProviderWorkCaptureProfile(t.Context(), path, digest, true); err == nil || loaded != nil {
		t.Fatal("launch reader skipped physical custody after portable decode", loaded, err)
	}
}

// All profile bytes remain under exact hash, duplicate-key and unknown-field
// admission before a caller may select one owner from the complete roster.
func TestProviderWholeWorkPortableProfileRefusesReinterpretedAuthority(t *testing.T) {
	profile := portableProviderWorkCaptureFixture(t)
	encode := func(value ProviderWorkCaptureProfile) []byte {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	original := encode(profile)
	for _, mutation := range []struct {
		name   string
		change func(*ProviderWorkCaptureProfile)
	}{
		{name: "schema", change: func(value *ProviderWorkCaptureProfile) { value.Schema += "-other" }},
		{name: "request authority", change: func(value *ProviderWorkCaptureProfile) { value.RequestPublicKey = [32]byte{} }},
		{name: "self approval", change: func(value *ProviderWorkCaptureProfile) { value.RequestPublicKey = value.Providers[0].PublicKey }},
		{name: "empty census", change: func(value *ProviderWorkCaptureProfile) { value.Providers = nil }},
		{name: "repeated slot", change: func(value *ProviderWorkCaptureProfile) { value.Providers[1].Slot = value.Providers[0].Slot }},
		{name: "repeated client", change: func(value *ProviderWorkCaptureProfile) { value.Providers[1].ClientId = value.Providers[0].ClientId }},
		{name: "absent client", change: func(value *ProviderWorkCaptureProfile) { value.Providers[0].ClientId = [16]byte{} }},
		{name: "absent client key", change: func(value *ProviderWorkCaptureProfile) { value.Providers[0].PublicKey = [32]byte{} }},
		{name: "incomplete domain", change: func(value *ProviderWorkCaptureProfile) { value.Providers[0].Domain.GenesisHash = [32]byte{} }},
		{name: "operator route", change: func(value *ProviderWorkCaptureProfile) { value.ApiUrl += "/work" }},
		{name: "relative outbox", change: func(value *ProviderWorkCaptureProfile) { value.Providers[0].OutboxDirectory = "original-direct" }},
		{name: "unclean outbox", change: func(value *ProviderWorkCaptureProfile) {
			value.Providers[0].OutboxDirectory += string(filepath.Separator) + "."
		}},
		{name: "nested outbox", change: func(value *ProviderWorkCaptureProfile) {
			value.Providers[1].OutboxDirectory = filepath.Join(value.Providers[0].OutboxDirectory, "nested")
		}},
	} {
		changed := profile
		changed.Providers = append([]ProviderWorkCaptureOwner(nil), profile.Providers...)
		mutation.change(&changed)
		raw := encode(changed)
		if loaded, err := DecodeProviderWorkCaptureProfile(t.Context(), raw, portableProviderWorkCaptureDigest(raw)); err == nil || loaded != nil {
			t.Fatal("portable admission accepted changed authority grammar", mutation.name, loaded, err)
		}
	}
	for _, raw := range [][]byte{
		bytes.Replace(original, []byte(`"schema":`), []byte(`"schema":"other","schema":`), 1),
		bytes.Replace(original, []byte(`"slot":"direct"`), []byte(`"slot":"direct","SLOT":"other"`), 1),
		append([]byte(`{"unknown":true,`), original[1:]...),
		append(bytes.Clone(original), []byte(` {}`)...),
		[]byte(`null`),
	} {
		if loaded, err := DecodeProviderWorkCaptureProfile(t.Context(), raw, portableProviderWorkCaptureDigest(raw)); err == nil || loaded != nil {
			t.Fatal("portable admission accepted ambiguous or foreign bytes", string(raw), loaded, err)
		}
	}
	digest := portableProviderWorkCaptureDigest(original)
	for _, supplied := range []string{"", strings.ToUpper(digest), strings.TrimPrefix(digest, "sha256:"), "sha256:" + strings.Repeat("x", 64), portableProviderWorkCaptureDigest(append(bytes.Clone(original), '\n'))} {
		if loaded, err := DecodeProviderWorkCaptureProfile(t.Context(), original, supplied); err == nil || loaded != nil {
			t.Fatal("portable admission replaced the approved exact digest", supplied, loaded, err)
		}
	}
}

// Offline decode has a finite byte budget and retains cancellation ownership.
func TestProviderWholeWorkPortableProfileBoundsAndCancellation(t *testing.T) {
	profile := portableProviderWorkCaptureFixture(t)
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	digest := portableProviderWorkCaptureDigest(raw)
	if loaded, err := DecodeProviderWorkCaptureProfile(nil, raw, digest); err == nil || loaded != nil {
		t.Fatal("portable admission detached from its caller", loaded, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if loaded, err := DecodeProviderWorkCaptureProfile(ctx, raw, digest); !errors.Is(err, context.Canceled) || loaded != nil {
		t.Fatal("portable admission discarded cancellation", loaded, err)
	}
	for _, bounded := range [][]byte{nil, append(bytes.Clone(raw), bytes.Repeat([]byte{' '}, maximumProviderWorkCaptureBytes-len(raw)+1)...)} {
		if loaded, err := DecodeProviderWorkCaptureProfile(t.Context(), bounded, portableProviderWorkCaptureDigest(bounded)); err == nil || loaded != nil {
			t.Fatal("portable admission accepted absent or excessive bytes", len(bounded), loaded, err)
		}
	}
}
