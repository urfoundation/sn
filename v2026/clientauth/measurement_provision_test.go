//go:build linux || darwin

// Real custody and HTTP qualify first provisioning: one created client, crash
// resumption before any send, replay after it, and refusal of every remnant.
package clientauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"testing"
)

// A private directory holding only the operator's network login, the state a
// first provisioning starts from.
func newMeasurementProvisioningFixture(t *testing.T) (*registrationTestFixture, string) {
	t.Helper()
	fixture := newRegistrationTestFixture(t)
	dir := filepath.Dir(fixture.clientPath)
	if err := os.Remove(fixture.networkPath); err != nil {
		t.Fatal(err)
	}
	fixture.clientPath = filepath.Join(dir, ".validator.jwt")
	fixture.networkPath = filepath.Join(dir, "jwt")
	if err := WriteToken(fixture.networkPath, registrationTestToken(t, "", "original")); err != nil {
		t.Fatal(err)
	}
	return fixture, filepath.Join(dir, ".validator.key")
}

// Every entry and its bytes, except the lock that opening the store creates.
func measurementDirectoryState(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]string{}
	for _, entry := range entries {
		if entry.Name() == ".validator.key.registration.lock" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		state[entry.Name()] = string(raw)
	}
	return state
}

func crashMeasurementProvisioningAfterKey(t *testing.T, keyPath string) []byte {
	t.Helper()
	stop := errors.New("synthetic crash after the measurement key fsync")
	owner, err := provisionValidatorMeasurementClientKey(t.Context(), keyPath, measurementProvisionHooks{afterKey: func() error { return stop }})
	if !errors.Is(err, stop) || owner != nil {
		t.Fatal("measurement key crash did not stop after durable publication", err)
	}
	seed, err := os.ReadFile(keyPath)
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatal("measurement key crash escaped before the durable key", err)
	}
	return seed
}

func TestMeasurementProvisioningCreatesExactlyOneClient(t *testing.T) {
	fixture, keyPath := newMeasurementProvisioningFixture(t)
	// A refused plain run leaves only the store's own lock behind.
	plain, err := OpenValidatorMeasurementClientKey(t.Context(), keyPath, false)
	var refused *RegistrationRefusedError
	if plain != nil || !errors.As(err, &refused) || refused.Code != "measurement_key_requires_explicit_recovery" {
		t.Fatal("pristine measurement directory opened without provisioning", err)
	}
	owner, err := ProvisionValidatorMeasurementClientKey(t.Context(), keyPath)
	if err != nil {
		t.Fatal(err)
	}
	seed := owner.Seed()
	marker := measurementKeyMarker(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	for _, name := range []string{".validator.key.identity", ".validator.key.provisioning"} {
		if raw, err := os.ReadFile(filepath.Join(filepath.Dir(keyPath), name)); err != nil || !bytes.Equal(raw, marker) {
			t.Fatal("measurement provisioning did not publish its marker", name, err)
		}
	}
	if raw, err := os.ReadFile(keyPath); err != nil || !bytes.Equal(raw, seed) {
		t.Fatal("measurement provisioning did not publish its key", err)
	}
	token, id, err := owner.ProvisionClientJwt(t.Context(), fixture.api, fixture.networkPath)
	if err != nil || token == "" || id.String() != testClientId || fixture.postCount() != 1 {
		t.Fatal("measurement provisioning did not create its one direct client", err)
	}
	assertMeasurementProvisioningSpent(t, keyPath, marker)
	if _, _, err := owner.ProvisionClientJwt(t.Context(), fixture.api, fixture.networkPath); err == nil || fixture.postCount() != 1 {
		t.Fatal("spent measurement provisioning authority created again", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := ProvisionValidatorMeasurementClientKey(t.Context(), keyPath)
	if again != nil || !errors.As(err, &refused) || refused.Code != "measurement_provisioning_requires_pristine_directory" {
		t.Fatal("provisioned measurement directory was provisioned again", err)
	}
	// The plain runner takes over the created client unchanged.
	plain = openMeasurementFixtureOwner(t, keyPath, false)
	refreshed, id, err := plain.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath)
	if err != nil || refreshed == "" || id.String() != testClientId || fixture.postCount() != 1 {
		t.Fatal("plain measurement run did not take over the provisioned client", err)
	}
	assertMeasurementProvisioningSpent(t, keyPath, marker)
}

// The authority is spent on disk: the same bytes now provisioned, nothing
// left to provision with.
func assertMeasurementProvisioningSpent(t *testing.T, keyPath string, marker []byte) {
	t.Helper()
	dir := filepath.Dir(keyPath)
	if raw, err := os.ReadFile(filepath.Join(dir, ".validator.key.provisioned")); err != nil || !bytes.Equal(raw, marker) {
		t.Fatal("measurement provisioning was not kept as provisioned provenance", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".validator.key.provisioning")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed measurement provisioning kept its creation authority", err)
	}
}

// After the one creation, losing the registration, its anchor and the client
// credential leaves a provisioned identity, never a resumable provisioning.
func TestMeasurementProvisionedIdentityLossCreatesNoSecondClient(t *testing.T) {
	fixture, keyPath := newMeasurementProvisioningFixture(t)
	owner, err := ProvisionValidatorMeasurementClientKey(t.Context(), keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := owner.ProvisionClientJwt(t.Context(), fixture.api, fixture.networkPath); err != nil || fixture.postCount() != 1 {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".validator.jwt.registration", ".validator.jwt.registration.started", ".validator.jwt"} {
		if err := os.Remove(filepath.Join(filepath.Dir(keyPath), name)); err != nil {
			t.Fatal(err)
		}
	}
	again, err := ProvisionValidatorMeasurementClientKey(t.Context(), keyPath)
	var refused *RegistrationRefusedError
	if again != nil || !errors.As(err, &refused) || refused.Code != "measurement_provisioning_requires_pristine_directory" {
		t.Fatal("a provisioned identity resumed provisioning after losing its registration", err)
	}
	plain := openMeasurementFixtureOwner(t, keyPath, false)
	if _, _, err := plain.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath); !errors.As(err, &refused) || refused.Code != "legacy_identity_requires_explicit_recovery" {
		t.Fatal("a provisioned identity's lost registration became an allocation", err)
	}
	if _, _, err := plain.loadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath, true); err == nil {
		t.Fatal("a provisioned marker authorized a second creation")
	}
	if fixture.postCount() != 1 {
		t.Fatalf("a provisioned identity made %d registrations", fixture.postCount())
	}
}

// A crash after the registration completes but before the marker rename is
// finished by the plain run path, which spends the authority itself.
func TestMeasurementProvisioningCrashBeforeSpendIsCompletedByThePlainPath(t *testing.T) {
	fixture, keyPath := newMeasurementProvisioningFixture(t)
	stop := errors.New("synthetic crash after the provisioned registration")
	owner, err := provisionValidatorMeasurementClientKey(t.Context(), keyPath, measurementProvisionHooks{afterRegistration: func() error { return stop }})
	if err != nil {
		t.Fatal(err)
	}
	marker := measurementKeyMarker(ed25519.NewKeyFromSeed(owner.Seed()).Public().(ed25519.PublicKey))
	if _, _, err := owner.ProvisionClientJwt(t.Context(), fixture.api, fixture.networkPath); !errors.Is(err, stop) || fixture.postCount() != 1 {
		t.Fatal("provisioned registration did not stop after completing", err)
	}
	dir := filepath.Dir(keyPath)
	for _, name := range []string{".validator.jwt.registration", ".validator.jwt", ".validator.key.provisioning"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal("the crash point is not after a retained registration and persisted credential", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".validator.key.provisioned")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the authority was spent before the crash point", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	plain := openMeasurementFixtureOwner(t, keyPath, false)
	if _, id, err := plain.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath); err != nil || id.String() != testClientId || fixture.postCount() != 1 {
		t.Fatal("plain run did not complete the provisioned registration", err)
	}
	assertMeasurementProvisioningSpent(t, keyPath, marker)
	// A leftover marker beside its provisioned copy is spent the same way; a
	// marker naming another key is a custody fault.
	if err := os.WriteFile(filepath.Join(dir, ".validator.key.provisioning"), marker, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := plain.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath); err != nil {
		t.Fatal(err)
	}
	assertMeasurementProvisioningSpent(t, keyPath, marker)
	other := measurementKeyMarker(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{32}, ed25519.SeedSize)).Public().(ed25519.PublicKey))
	if err := os.WriteFile(filepath.Join(dir, ".validator.key.provisioning"), other, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := plain.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath); err == nil {
		t.Fatal("a provisioning marker for another key was spent")
	}
	if raw, err := os.ReadFile(filepath.Join(dir, ".validator.key.provisioned")); err != nil || !bytes.Equal(raw, marker) || fixture.postCount() != 1 {
		t.Fatal("a foreign provisioning marker replaced the provisioned provenance", err)
	}
}

func TestMeasurementProvisioningResumesAfterKeyCrash(t *testing.T) {
	fixture, keyPath := newMeasurementProvisioningFixture(t)
	seed := crashMeasurementProvisioningAfterKey(t, keyPath)
	// The plain run path still cannot create, and its refused attempt leaves
	// the client credential lock behind.
	plain, err := OpenValidatorMeasurementClientKey(t.Context(), keyPath, false)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = plain.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath)
	var refused *RegistrationRefusedError
	if !errors.As(err, &refused) || refused.Code != "legacy_identity_requires_explicit_recovery" || fixture.postCount() != 0 {
		t.Fatal("plain measurement run created a client", err)
	}
	if _, _, err := plain.ProvisionClientJwt(t.Context(), fixture.api, fixture.networkPath); err == nil || fixture.postCount() != 0 {
		t.Fatal("opened measurement owner borrowed provisioning authority", err)
	}
	if err := plain.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := ProvisionValidatorMeasurementClientKey(t.Context(), keyPath)
	if err != nil {
		t.Fatal("interrupted measurement provisioning did not resume", err)
	}
	defer resumed.Close()
	if !bytes.Equal(resumed.Seed(), seed) {
		t.Fatal("resumed measurement provisioning replaced its key")
	}
	if _, id, err := resumed.ProvisionClientJwt(t.Context(), fixture.api, fixture.networkPath); err != nil || id.String() != testClientId || fixture.postCount() != 1 {
		t.Fatal("resumed measurement provisioning did not create its one client", err)
	}
}

func TestMeasurementProvisioningReplaysAfterFirstSend(t *testing.T) {
	fixture, keyPath := newMeasurementProvisioningFixture(t)
	owner, err := ProvisionValidatorMeasurementClientKey(t.Context(), keyPath)
	if err != nil {
		t.Fatal(err)
	}
	marker := measurementKeyMarker(ed25519.NewKeyFromSeed(owner.Seed()).Public().(ed25519.PublicKey))
	ctx, cancel := context.WithCancel(t.Context())
	fixture.onCommit = cancel
	if _, _, err := owner.ProvisionClientJwt(ctx, fixture.api, fixture.networkPath); err == nil || fixture.postCount() != 1 {
		t.Fatal("measurement provisioning lost reply did not cross the HTTP commit", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(keyPath), ".validator.key.provisioned")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a lost reply spent the provisioning authority before the registration completed", err)
	}
	original, err := os.ReadFile(fixture.clientPath + ".registration")
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := ProvisionValidatorMeasurementClientKey(t.Context(), keyPath)
	var refused *RegistrationRefusedError
	if again != nil || !errors.As(err, &refused) || refused.Code != "measurement_provisioning_requires_pristine_directory" {
		t.Fatal("sent measurement provisioning was provisioned again", err)
	}
	plain := openMeasurementFixtureOwner(t, keyPath, false)
	token, id, err := plain.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath)
	if err != nil || token == "" || id.String() != testClientId || fixture.postCount() != 2 {
		t.Fatal("measurement provisioning lost reply did not replay its original operation", err)
	}
	var before, completed registrationRecord
	after, err := os.ReadFile(fixture.clientPath + ".registration")
	if err != nil || json.Unmarshal(original, &before) != nil || json.Unmarshal(after, &completed) != nil || before.Request != completed.Request || before.Scope != completed.Scope || completed.ClientId != testClientId {
		t.Fatal("measurement provisioning replay changed its retained request")
	}
	// The replay that completed the registration also spent the authority.
	assertMeasurementProvisioningSpent(t, keyPath, marker)
}

func TestMeasurementProvisioningRefusesEveryRemnant(t *testing.T) {
	seed := bytes.Repeat([]byte{29}, ed25519.SeedSize)
	marker := measurementKeyMarker(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	remnants := []struct {
		name string
		raw  []byte
	}{
		{name: ".validator.key", raw: seed},
		{name: ".validator.key.identity", raw: marker},
		{name: ".validator.key.provisioning", raw: marker},
		{name: ".validator.jwt.registration", raw: []byte("synthetic retained request")},
		{name: ".validator.jwt.registration.started", raw: []byte("synthetic first-send anchor")},
		{name: ".validator.jwt.registration.existing", raw: []byte("synthetic existing client")},
		{name: ".validator.jwt", raw: []byte("synthetic client credential")},
		{name: ".validator.jwt.rejected", raw: []byte("blocked")},
		{name: ".validator.retained", raw: []byte("synthetic unknown history")},
		{name: ".validator.key.provisioned", raw: marker},
	}
	root := t.TempDir()
	prepare := func(label string, files map[string][]byte) string {
		dir := filepath.Join(root, label)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for name, raw := range files {
			if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	refuses := func(label string, dir string) {
		before := measurementDirectoryState(t, dir)
		owner, err := ProvisionValidatorMeasurementClientKey(t.Context(), filepath.Join(dir, ".validator.key"))
		var refused *RegistrationRefusedError
		if owner != nil || !errors.As(err, &refused) || refused.Code != "measurement_provisioning_requires_pristine_directory" {
			t.Fatalf("measurement remnant %s was provisioned: %v", label, err)
		}
		if after := measurementDirectoryState(t, dir); !maps.Equal(before, after) {
			t.Fatalf("refused measurement remnant %s changed its directory", label)
		}
	}
	// Only the empty set and the key with both markers are provisioning states.
	resumable := 1<<0 | 1<<1 | 1<<2
	for set := 1; set < 1<<len(remnants); set++ {
		if set == resumable {
			continue
		}
		files := map[string][]byte{}
		for index, remnant := range remnants {
			if set&(1<<index) != 0 {
				files[remnant.name] = remnant.raw
			}
		}
		label := fmt.Sprintf("set-%03x", set)
		refuses(label, prepare(label, files))
	}
	other := measurementKeyMarker(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{30}, ed25519.SeedSize)).Public().(ed25519.PublicKey))
	for index, fault := range []struct {
		name string
		raw  []byte
	}{
		{name: ".validator.key", raw: make([]byte, ed25519.SeedSize)},
		{name: ".validator.key", raw: seed[:ed25519.SeedSize-1]},
		{name: ".validator.key.identity", raw: other},
		{name: ".validator.key.provisioning", raw: other},
	} {
		files := map[string][]byte{".validator.key": seed, ".validator.key.identity": marker, ".validator.key.provisioning": marker}
		files[fault.name] = fault.raw
		label := fmt.Sprintf("mismatch-%d", index)
		refuses(label, prepare(label, files))
	}
	// A crash before the key leaves a keyless remnant; the plain path keeps
	// today's refusal for it.
	for _, files := range []map[string][]byte{
		{".validator.key.provisioning": marker},
		{".validator.key.provisioning": marker, ".validator.key.identity": marker},
	} {
		dir := prepare(fmt.Sprintf("keyless-%d", len(files)), files)
		owner, err := OpenValidatorMeasurementClientKey(t.Context(), filepath.Join(dir, ".validator.key"), false)
		var refused *RegistrationRefusedError
		if owner != nil || !errors.As(err, &refused) || refused.Code != "measurement_key_requires_explicit_recovery" {
			t.Fatal("keyless measurement provisioning remnant opened", err)
		}
	}
}

// The registration guard itself requires this owner's directory to hold a
// provisioning marker naming the key, whatever the caller asks for.
func TestMeasurementCreationRequiresProvisioningMarker(t *testing.T) {
	fixture, keyPath := newMeasurementProvisioningFixture(t)
	crashMeasurementProvisioningAfterKey(t, keyPath)
	markerPath := filepath.Join(filepath.Dir(keyPath), ".validator.key.provisioning")
	if err := os.Remove(markerPath); err != nil {
		t.Fatal(err)
	}
	owner := openMeasurementFixtureOwner(t, keyPath, false)
	if _, _, err := owner.loadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath, true); err == nil || fixture.postCount() != 0 {
		t.Fatal("measurement creation passed the guard without a provisioning marker", err)
	}
	other := measurementKeyMarker(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{31}, ed25519.SeedSize)).Public().(ed25519.PublicKey))
	if err := os.WriteFile(markerPath, other, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := owner.loadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath, true); err == nil || fixture.postCount() != 0 {
		t.Fatal("measurement creation passed the guard with another key's marker", err)
	}
	// A provisioned marker is spent authority, alone or beside a provisioning
	// marker for the same key.
	marker := measurementKeyMarker(ed25519.NewKeyFromSeed(owner.Seed()).Public().(ed25519.PublicKey))
	provisionedPath := filepath.Join(filepath.Dir(keyPath), ".validator.key.provisioned")
	if err := os.Remove(markerPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provisionedPath, marker, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := owner.loadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath, true); err == nil || fixture.postCount() != 0 {
		t.Fatal("measurement creation passed the guard with a provisioned marker", err)
	}
	if err := os.WriteFile(markerPath, marker, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := owner.loadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath, true); err == nil || fixture.postCount() != 0 {
		t.Fatal("measurement creation passed the guard beside a provisioned marker", err)
	}
	if _, err := os.Stat(fixture.clientPath + ".registration"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused measurement creation retained a request", err)
	}
}
