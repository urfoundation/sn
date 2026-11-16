//go:build linux || darwin

// Real private custody and the qualified versioned HTTP fixture cover provider
// scope and first-key ownership. Every identity and credential is synthetic.
package clientauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Custody fixtures set their own privacy instead of inheriting the test
// process's umask. Negative cases must reach their intended custody guard.
func providerRegistrationPrivateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Only the actual purpose/key/endpoint/slot select this provider operation.
func providerRegistrationTestScope(fixture *registrationTestFixture) RegistrationScope {
	return RegistrationScope{Endpoint: fixture.scope.Endpoint, ClientKey: "0x" + strings.Repeat("34", 32), ClientRole: "provider-v1", ClientSlot: "direct"}
}

// Adding a provider variant cannot reinterpret historical validator records.
func TestProviderRegistrationPreservesValidatorScopeBytes(t *testing.T) {
	fixture := newRegistrationTestFixture(t)
	raw, err := json.Marshal(fixture.scope)
	legacy, legacyErr := json.Marshal(struct {
		Endpoint     string `json:"endpoint"`
		DeploymentId string `json:"deployment_id"`
		ChainId      uint64 `json:"chain_id"`
		GenesisHash  string `json:"genesis_hash"`
		Netuid       uint16 `json:"netuid"`
		ValidatorId  uint64 `json:"validator_id"`
		OperatorNoId uint64 `json:"operator_no_id"`
		ClientKey    string `json:"client_key"`
	}{Endpoint: fixture.scope.Endpoint, DeploymentId: fixture.scope.DeploymentId, ChainId: fixture.scope.ChainId, GenesisHash: fixture.scope.GenesisHash, Netuid: fixture.scope.Netuid, ValidatorId: fixture.scope.ValidatorId, OperatorNoId: fixture.scope.OperatorNoId, ClientKey: fixture.scope.ClientKey})
	if err != nil || legacyErr != nil || !bytes.Equal(raw, legacy) {
		t.Fatal("provider scope extension changed historical validator bytes")
	}
}

// Real provider allocation has no synthetic validator or chain fields; mixed
// scopes are refused before a request or any new operation custody is created.
func TestProviderRegistrationSeparatesRoleAndSlotAuthority(t *testing.T) {
	for _, fault := range []string{"valid", "validator", "operator", "chain", "genesis", "deployment", "unknown-role", "empty-slot", "bad-proxy"} {
		fixture := newRegistrationTestFixture(t)
		fixture.scope = providerRegistrationTestScope(fixture)
		switch fault {
		case "validator":
			fixture.scope.ValidatorId = 1
		case "operator":
			fixture.scope.OperatorNoId = 1
		case "chain":
			fixture.scope.ChainId = 964
		case "genesis":
			fixture.scope.GenesisHash = "0x" + strings.Repeat("12", 32)
		case "deployment":
			fixture.scope.DeploymentId = "synthetic-chain-deployment"
		case "unknown-role":
			fixture.scope.ClientRole = "approved-validator"
		case "empty-slot":
			fixture.scope.ClientSlot = ""
		case "bad-proxy":
			fixture.scope.ClientSlot = "proxy-sha256:12"
		}
		token, _, err := LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath, fixture.clientPath, "provider", fixture.scope, true)
		if fault == "valid" {
			if err != nil || token == "" || fixture.postCount() != 1 {
				t.Fatalf("provider role did not use its actual versioned authority: %v", err)
			}
		} else if err == nil || token != "" || fixture.postCount() != 0 {
			t.Fatalf("provider %s scope acquired registration authority: %v", fault, err)
		}
	}
}

// Request and binding crash points already perform their real fsync/rename;
// reopening the provider role must retain that same opaque server request.
func TestProviderRegistrationRetainsRequestAndBindingAcrossCrashes(t *testing.T) {
	for _, phase := range []string{"request", "binding"} {
		fixture := newRegistrationTestFixture(t)
		fixture.scope = providerRegistrationTestScope(fixture)
		stopped := errors.New("synthetic provider crash after durable " + phase)
		hooks := registrationHooks{}
		if phase == "request" {
			hooks.afterRequest = func() error { return stopped }
		} else {
			hooks.afterBinding = func() error { return stopped }
		}
		if token, _, err := loadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath, fixture.clientPath, "provider", fixture.scope, true, hooks); !errors.Is(err, stopped) || token != "" {
			t.Fatal("provider crash did not follow its actual durable boundary", err)
		}
		before, err := os.ReadFile(fixture.clientPath + ".registration")
		if err != nil {
			t.Fatal(err)
		}
		var original registrationRecord
		if err := json.Unmarshal(before, &original); err != nil {
			t.Fatal(err)
		}
		if phase == "binding" && (original.ClientId == "" || original.DeviceId == "") {
			t.Fatal("provider binding crash escaped before durable server identity")
		}
		token, _, err := LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath, fixture.clientPath, "provider", fixture.scope, false)
		if err != nil || token == "" {
			t.Fatal("provider retained operation required new creation permission", err)
		}
		after, err := os.ReadFile(fixture.clientPath + ".registration")
		var recovered registrationRecord
		if err != nil || json.Unmarshal(after, &recovered) != nil || original.Request != recovered.Request || original.Scope != recovered.Scope {
			t.Fatal("provider restart replaced original scope or server request")
		}
	}
}

// Retained files cannot be reinterpreted as another key, endpoint, domain or
// proxy slot, even when a caller deliberately aliases its credential path.
func TestProviderRegistrationRefusesChangedRetainedScope(t *testing.T) {
	for _, fault := range []string{"key", "endpoint", "slot", "role", "description"} {
		fixture := newRegistrationTestFixture(t)
		fixture.scope = providerRegistrationTestScope(fixture)
		stop := errors.New("synthetic stop after original provider request")
		_, _, err := loadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath, fixture.clientPath, "provider", fixture.scope, true, registrationHooks{afterRequest: func() error { return stop }})
		if !errors.Is(err, stop) {
			t.Fatal(err)
		}
		original, err := os.ReadFile(fixture.clientPath + ".registration")
		if err != nil {
			t.Fatal(err)
		}
		description := "provider"
		switch fault {
		case "key":
			fixture.scope.ClientKey = "0x" + strings.Repeat("56", 32)
		case "endpoint":
			fixture.scope.Endpoint += "/changed"
		case "slot":
			fixture.scope.ClientSlot = "proxy-sha256:" + strings.Repeat("78", 32)
		case "role":
			fixture.scope.ClientRole = "synthetic-other-role"
		case "description":
			description = "provider synthetic-build-change"
		}
		token, _, err := LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath, fixture.clientPath, description, fixture.scope, true)
		after, readErr := os.ReadFile(fixture.clientPath + ".registration")
		if err == nil || token != "" || fixture.postCount() != 0 || readErr != nil || !bytes.Equal(original, after) {
			t.Fatal("changed provider scope replaced its retained operation", fault, err)
		}
	}
}

// The key exists privately and its public marker is durable before any caller
// may construct a registration. The global owner spans all proxy credentials.
func TestProviderClientKeyCreationAndLifetimeAreExplicit(t *testing.T) {
	path := filepath.Join(providerRegistrationPrivateDir(t), ".provider.key")
	if owner, err := OpenProviderClientKey(t.Context(), path, ProviderClientKeyOptions{}); err == nil || owner != nil {
		t.Fatal("missing provider key consumed implicit creation authority")
	}
	owner, err := OpenProviderClientKey(t.Context(), path, ProviderClientKeyOptions{AllowCreate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Close() })
	seed := owner.Seed()
	raw, readErr := os.ReadFile(path)
	info, statErr := os.Stat(path)
	public := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	marker, markerErr := os.ReadFile(filepath.Join(filepath.Dir(path), ".provider.key.identity"))
	if readErr != nil || statErr != nil || markerErr != nil || !bytes.Equal(raw, seed) || info.Mode().Perm() != 0600 || !bytes.Contains(marker, []byte(hex.EncodeToString(public))) {
		t.Fatal("provider key escaped before durable private identity")
	}
	seed[0] ^= 1
	if bytes.Equal(seed, owner.Seed()) {
		t.Fatal("provider key owner exposed mutable retained seed")
	}
	if duplicate, err := OpenProviderClientKey(t.Context(), path, ProviderClientKeyOptions{AllowCreate: true}); err == nil || duplicate != nil {
		t.Fatal("second provider process acquired shared key custody")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenProviderClientKey(t.Context(), path, ProviderClientKeyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !bytes.Equal(raw, reopened.Seed()) {
		t.Fatal("provider restart generated another identity")
	}
}

// A crash after key publication but before marker publication retains that key;
// marker-only or other retained history must never imply a fresh installation.
func TestProviderClientKeyRecoversPublishedSeedAndRejectsMissingHistory(t *testing.T) {
	path := filepath.Join(providerRegistrationPrivateDir(t), ".provider.key")
	seed := bytes.Repeat([]byte{9}, ed25519.SeedSize)
	if err := os.WriteFile(path, seed, 0600); err != nil {
		t.Fatal(err)
	}
	owner, err := OpenProviderClientKey(t.Context(), path, ProviderClientKeyOptions{})
	if err != nil || !bytes.Equal(seed, owner.Seed()) {
		t.Fatal("published provider seed was not recovered", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".provider.key.identity", ".provider.jwt", ".provider.jwt.registration", ".provider.jwt.registration.started", ".provider.jwt.registration.existing", ".provider-0123456789abcdef.jwt", ".provider.unknown", ".provider.cert"} {
		path := filepath.Join(providerRegistrationPrivateDir(t), ".provider.key")
		if err := os.WriteFile(filepath.Join(filepath.Dir(path), name), []byte("synthetic retained identity"), 0600); err != nil {
			t.Fatal(err)
		}
		owner, err := OpenProviderClientKey(t.Context(), path, ProviderClientKeyOptions{AllowCreate: true})
		if err == nil || owner != nil {
			t.Fatalf("missing key with %s became a new provider", name)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("refused key recovery wrote a replacement seed")
		}
	}
}

// Interruption after the real seed fsync must reuse those bytes and complete
// the marker before any request can consume the returned public identity.
func TestProviderClientKeyCrashAfterSeedReusesOriginal(t *testing.T) {
	path := filepath.Join(providerRegistrationPrivateDir(t), ".provider.key")
	stop := errors.New("synthetic crash after provider seed fsync")
	owner, err := openProviderClientKey(t.Context(), path, ProviderClientKeyOptions{AllowCreate: true}, providerClientKeyHooks{afterSeed: func() error { return stop }})
	if !errors.Is(err, stop) || owner != nil {
		t.Fatal("provider seed crash did not stop after durable publication", err)
	}
	seed, err := os.ReadFile(path)
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatal("provider seed crash escaped before actual durable bytes")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), ".provider.key.identity")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("provider seed crash occurred after its marker")
	}
	owner, err = OpenProviderClientKey(t.Context(), path, ProviderClientKeyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if !bytes.Equal(seed, owner.Seed()) {
		t.Fatal("provider seed crash generated a replacement identity")
	}
}

// Missing public identity plus retained history is never a bare-key crash.
// A first-upgrade assertion cannot erase versioned records or earlier adoption.
func TestProviderClientKeyMissingMarkerDoesNotBlessRetainedHistory(t *testing.T) {
	for _, name := range []string{".provider.jwt.registration", ".provider.jwt.registration.started", ".provider.jwt.registration.existing", ".provider.jwt.registration.lock", ".provider.jwt.rejected", ".provider.unknown"} {
		path := filepath.Join(providerRegistrationPrivateDir(t), ".provider.key")
		if err := os.WriteFile(path, bytes.Repeat([]byte{19}, ed25519.SeedSize), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(path), name), []byte("synthetic retained history"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, options := range []ProviderClientKeyOptions{{}, {AllowCreate: true}, {AdoptLegacyKey: true}} {
			owner, err := OpenProviderClientKey(t.Context(), path, options)
			if err == nil || owner != nil {
				t.Fatal("missing provider marker blessed retained identity history", name)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), ".provider.key.identity")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("refused provider marker recovery manufactured new custody")
			}
		}
	}
	if owner, err := OpenProviderClientKey(t.Context(), filepath.Join(providerRegistrationPrivateDir(t), ".provider.key"), ProviderClientKeyOptions{AllowCreate: true, AdoptLegacyKey: true}); err == nil || owner != nil {
		t.Fatal("provider legacy key adoption also granted new creation")
	}
}

// Unsafe, malformed and changed keys fail even with a new-create flag. The
// namespace guards must execute before reading key bytes or replacing files.
func TestProviderClientKeyRejectsChangedOrUnsafeCustody(t *testing.T) {
	for _, fault := range []string{"short", "zero", "mode", "symlink", "hardlink", "marker"} {
		path := filepath.Join(providerRegistrationPrivateDir(t), ".provider.key")
		seed := bytes.Repeat([]byte{7}, ed25519.SeedSize)
		if fault == "short" {
			seed = seed[:31]
		}
		if fault == "zero" {
			seed = make([]byte, ed25519.SeedSize)
		}
		if err := os.WriteFile(path, seed, 0600); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "mode":
			if err := os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			if err := os.Rename(path, path+".saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path+".saved", path); err != nil {
				t.Fatal(err)
			}
		case "hardlink":
			if err := os.Link(path, path+".link"); err != nil {
				t.Fatal(err)
			}
		case "marker":
			if err := os.WriteFile(filepath.Join(filepath.Dir(path), ".provider.key.identity"), []byte("synthetic foreign key"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if owner, err := OpenProviderClientKey(t.Context(), path, ProviderClientKeyOptions{AllowCreate: true}); err == nil || owner != nil {
			t.Fatalf("provider %s custody was admitted", fault)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if owner, err := OpenProviderClientKey(ctx, filepath.Join(providerRegistrationPrivateDir(t), ".provider.key"), ProviderClientKeyOptions{AllowCreate: true}); !errors.Is(err, context.Canceled) || owner != nil {
		t.Fatal("canceled key preparation created custody")
	}
}
