//go:build linux || darwin

// Real custody and HTTP qualify the measurement-only role. Synthetic retained
// operations model separately recovered originals, never new-run authority.
package clientauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/sdk/v2026"
)

func newMeasurementRegistrationFixture(t *testing.T) (*registrationTestFixture, string) {
	t.Helper()
	fixture := newRegistrationTestFixture(t)
	dir := filepath.Dir(fixture.clientPath)
	fixture.clientPath = filepath.Join(dir, ".validator.jwt")
	fixture.networkPath = filepath.Join(dir, "jwt")
	if err := WriteToken(fixture.networkPath, registrationTestToken(t, "", "original")); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, ".validator.key")
	seed := bytes.Repeat([]byte{27}, ed25519.SeedSize)
	if err := os.WriteFile(keyPath, seed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteToken(fixture.clientPath, registrationTestToken(t, testClientId, "legacy")); err != nil {
		t.Fatal(err)
	}
	public := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	fixture.scope = RegistrationScope{Endpoint: fixture.scope.Endpoint, ClientKey: "0x" + hex.EncodeToString(public), ClientRole: "validator-measurement-v1", ClientSlot: "direct"}
	return fixture, keyPath
}

func openMeasurementFixtureOwner(t *testing.T, keyPath string, adopt bool) *ValidatorMeasurementClientKeyOwner {
	t.Helper()
	owner, err := OpenValidatorMeasurementClientKey(t.Context(), keyPath, adopt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	return owner
}

// Explicit test preparation supplies the original request; the public owner
// under test is incapable of creating it. The real loader validates all bytes.
func retainMeasurementFixtureOperation(t *testing.T, fixture *registrationTestFixture) []byte {
	t.Helper()
	bootstrap, err := ReadToken(fixture.networkPath)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := registrationIdentity(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := json.Marshal(fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(scope)
	record := &registrationRecord{Schema: registrationRecordSchema, Scope: fixture.scope, Principal: identity.registrationPrincipal, Request: sdk.RegisterNetworkClientArgs{Schema: sdk.NetworkClientRegistrationSchema, RegistrationId: strings.Repeat("45", 32), ScopeSha256: hex.EncodeToString(digest[:]), DeviceDescription: "validator measurement"}}
	if err := validateRegistrationRecord(record, fixture.scope, "validator measurement"); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	store, err := openRegistrationStore(fixture.clientPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if err := store.remove(filepath.Base(fixture.clientPath)); err != nil {
		t.Fatal(err)
	}
	if err := store.write(filepath.Base(fixture.clientPath)+".registration", raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestMeasurementRegistrationPreservesProviderAndProductionScopeBytes(t *testing.T) {
	endpoint := "https://api.synthetic.example/network/register-client-v1"
	key := "0x" + strings.Repeat("34", 32)
	provider := RegistrationScope{Endpoint: endpoint, ClientKey: key, ClientRole: "provider-v1", ClientSlot: "direct"}
	production := RegistrationScope{Endpoint: endpoint, DeploymentId: "synthetic-deployment", ChainId: 964, GenesisHash: "0x" + strings.Repeat("12", 32), Netuid: 71, ValidatorId: 1, OperatorNoId: 9, ClientKey: key}
	wants := []string{`{"endpoint":"` + endpoint + `","deployment_id":"","chain_id":0,"genesis_hash":"","netuid":0,"validator_id":0,"operator_no_id":0,"client_key":"` + key + `","client_role":"provider-v1","client_slot":"direct"}`, `{"endpoint":"` + endpoint + `","deployment_id":"synthetic-deployment","chain_id":964,"genesis_hash":"0x` + strings.Repeat("12", 32) + `","netuid":71,"validator_id":1,"operator_no_id":9,"client_key":"` + key + `"}`}
	for i, scope := range []RegistrationScope{provider, production} {
		raw, err := json.Marshal(scope)
		if err != nil || string(raw) != wants[i] {
			t.Fatal("measurement scope changed provider or production validator wire bytes", i, err)
		}
	}
}

func TestMeasurementRegistrationRejectsCreationAndInventedAuthority(t *testing.T) {
	for _, fault := range []string{"create", "proxy", "validator", "operator", "chain", "genesis", "netuid", "deployment", "unknown-role"} {
		fixture, _ := newMeasurementRegistrationFixture(t)
		scope := fixture.scope
		allow := false
		switch fault {
		case "create":
			allow = true
		case "proxy":
			scope.ClientSlot = "proxy-sha256:" + strings.Repeat("56", 32)
		case "validator":
			scope.ValidatorId = 1
		case "operator":
			scope.OperatorNoId = 1
		case "chain":
			scope.ChainId = 964
		case "genesis":
			scope.GenesisHash = "0x" + strings.Repeat("12", 32)
		case "netuid":
			scope.Netuid = 71
		case "deployment":
			scope.DeploymentId = "synthetic"
		case "unknown-role":
			scope.ClientRole = "validator-measurement-v2"
		}
		token, _, err := LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath, fixture.clientPath, "validator measurement", scope, allow)
		_, markerErr := os.Stat(fixture.clientPath + ".registration.existing")
		if err == nil || token != "" || fixture.postCount() != 0 || !errors.Is(markerErr, os.ErrNotExist) {
			t.Fatal("measurement role acquired invented registration authority", fault, err)
		}
	}
}

func TestMeasurementKeyRequiresOriginalSeedAndExplicitLegacyAssertion(t *testing.T) {
	for _, fault := range []string{"missing-seed", "short-seed", "zero-seed", "missing-client", "no-assertion"} {
		fixture, keyPath := newMeasurementRegistrationFixture(t)
		switch fault {
		case "missing-seed":
			if err := os.Remove(keyPath); err != nil {
				t.Fatal(err)
			}
		case "short-seed":
			if err := os.WriteFile(keyPath, []byte{1}, 0600); err != nil {
				t.Fatal(err)
			}
		case "zero-seed":
			if err := os.WriteFile(keyPath, make([]byte, 32), 0600); err != nil {
				t.Fatal(err)
			}
		case "missing-client":
			if err := os.Remove(fixture.clientPath); err != nil {
				t.Fatal(err)
			}
		}
		before, readErr := os.ReadFile(keyPath)
		owner, err := OpenValidatorMeasurementClientKey(t.Context(), keyPath, fault != "no-assertion")
		if owner != nil {
			_ = owner.Close()
		}
		after, afterErr := os.ReadFile(keyPath)
		_, markerErr := os.Stat(keyPath + ".identity")
		if err == nil || owner != nil || fixture.postCount() != 0 || !bytes.Equal(before, after) || errors.Is(readErr, os.ErrNotExist) != errors.Is(afterErr, os.ErrNotExist) || !errors.Is(markerErr, os.ErrNotExist) {
			t.Fatal("measurement key loss or unproven legacy key generated custody", fault, err)
		}
	}
}

func TestMeasurementRegistrationAdoptsLegacyWithoutBootstrapAndRetainsLoss(t *testing.T) {
	fixture, keyPath := newMeasurementRegistrationFixture(t)
	owner := openMeasurementFixtureOwner(t, keyPath, true)
	if err := os.Remove(fixture.networkPath); err != nil {
		t.Fatal(err)
	}
	token, id, err := owner.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath)
	marker, markerErr := os.ReadFile(fixture.clientPath + ".registration.existing")
	if err != nil || token == "" || id.String() != testClientId || markerErr != nil || !bytes.Contains(marker, []byte(`"client_role":"validator-measurement-v1"`)) || fixture.postCount() != 0 {
		t.Fatal("measurement legacy adoption did not retain its original direct client", err)
	}
	if err := os.Remove(fixture.clientPath); err != nil {
		t.Fatal(err)
	}
	_, _, err = owner.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath)
	var refused *RegistrationRefusedError
	after, readErr := os.ReadFile(fixture.clientPath + ".registration.existing")
	if !errors.As(err, &refused) || refused.Code != "legacy_identity_requires_explicit_recovery" || fixture.postCount() != 0 || readErr != nil || !bytes.Equal(marker, after) {
		t.Fatal("measurement lost legacy client became an allocation", err)
	}
}

func TestMeasurementRegistrationReplaysOriginalLostReplyWithoutCreate(t *testing.T) {
	fixture, keyPath := newMeasurementRegistrationFixture(t)
	owner := openMeasurementFixtureOwner(t, keyPath, true)
	original := retainMeasurementFixtureOperation(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	fixture.onCommit = cancel
	_, _, err := owner.LoadOrRegisterClientJwt(ctx, fixture.api, fixture.networkPath)
	if err == nil || fixture.postCount() != 1 {
		t.Fatal("measurement lost reply did not cross the original HTTP commit", err)
	}
	after, readErr := os.ReadFile(fixture.clientPath + ".registration")
	if readErr != nil || !bytes.Equal(original, after) {
		t.Fatal("measurement lost reply rewrote its original operation")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	owner = openMeasurementFixtureOwner(t, keyPath, false)
	token, id, err := owner.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath)
	if err != nil || token == "" || id.String() != testClientId || fixture.postCount() != 2 {
		t.Fatal("measurement original lost reply required fresh allocation authority", err)
	}
	bootstrapOwner, err := openRegistrationStore(fixture.networkPath)
	if err != nil {
		t.Fatal("completed measurement replay monopolized shared bootstrap custody", err)
	}
	if err := bootstrapOwner.close(); err != nil {
		t.Fatal(err)
	}
	var before, completed registrationRecord
	after, err = os.ReadFile(fixture.clientPath + ".registration")
	if err != nil || json.Unmarshal(original, &before) != nil || json.Unmarshal(after, &completed) != nil || before.Request != completed.Request || before.Scope != completed.Scope || completed.ClientId != testClientId {
		t.Fatal("measurement replay changed its retained scope or request")
	}
}

func TestMeasurementRegistrationMissingOperationRetainsAnchorRefusal(t *testing.T) {
	fixture, keyPath := newMeasurementRegistrationFixture(t)
	owner := openMeasurementFixtureOwner(t, keyPath, true)
	retainMeasurementFixtureOperation(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	fixture.onCommit = cancel
	_, _, _ = owner.LoadOrRegisterClientJwt(ctx, fixture.api, fixture.networkPath)
	anchor, err := os.ReadFile(fixture.clientPath + ".registration.started")
	if err != nil || fixture.postCount() != 1 {
		t.Fatal("measurement missing-operation fixture did not retain a sent anchor")
	}
	if err := os.Remove(fixture.clientPath + ".registration"); err != nil {
		t.Fatal(err)
	}
	_, _, err = owner.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath)
	after, readErr := os.ReadFile(fixture.clientPath + ".registration.started")
	var refused *RegistrationRefusedError
	if !errors.As(err, &refused) || refused.Code != "registration_custody_missing_after_send" || fixture.postCount() != 1 || readErr != nil || !bytes.Equal(anchor, after) {
		t.Fatal("measurement missing request was replaced after possible send", err)
	}
}

func TestMeasurementKeyLossCannotRebindRetainedRegistration(t *testing.T) {
	for _, fault := range []string{"marker", "seed", "replacement"} {
		fixture, keyPath := newMeasurementRegistrationFixture(t)
		owner := openMeasurementFixtureOwner(t, keyPath, true)
		original := retainMeasurementFixtureOperation(t, fixture)
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "marker":
			if err := os.Remove(keyPath + ".identity"); err != nil {
				t.Fatal(err)
			}
		case "seed":
			if err := os.Remove(keyPath); err != nil {
				t.Fatal(err)
			}
		case "replacement":
			if err := os.WriteFile(keyPath, bytes.Repeat([]byte{28}, 32), 0600); err != nil {
				t.Fatal(err)
			}
		}
		owner, err := OpenValidatorMeasurementClientKey(t.Context(), keyPath, true)
		if owner != nil {
			_ = owner.Close()
		}
		after, readErr := os.ReadFile(fixture.clientPath + ".registration")
		if err == nil || owner != nil || fixture.postCount() != 0 || readErr != nil || !bytes.Equal(original, after) {
			t.Fatal("measurement lost original key custody rebound retained work", fault, err)
		}
	}
}

func TestMeasurementKeyOwnerExcludesConcurrentClaimant(t *testing.T) {
	_, keyPath := newMeasurementRegistrationFixture(t)
	owner := openMeasurementFixtureOwner(t, keyPath, true)
	result := make(chan error, 1)
	go func() {
		other, err := OpenValidatorMeasurementClientKey(t.Context(), keyPath, false)
		if other != nil {
			_ = other.Close()
		}
		result <- err
	}()
	if err := <-result; err == nil {
		t.Fatal("measurement key admitted a concurrent claimant")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	owner = openMeasurementFixtureOwner(t, keyPath, false)
	if len(owner.Seed()) != 32 {
		t.Fatal("measurement owner did not reopen after joined release")
	}
}

// The retained token stays valid in this fixture: only missing key identity
// custody blocks re-adoption, rather than an unrelated missing-token check.
func TestMeasurementKeyMarkerLossCannotAdoptRetainedClient(t *testing.T) {
	for _, kind := range []string{"existing", "versioned"} {
		fixture, keyPath := newMeasurementRegistrationFixture(t)
		owner := openMeasurementFixtureOwner(t, keyPath, true)
		if kind == "versioned" {
			retainMeasurementFixtureOperation(t, fixture)
		}
		if _, _, err := owner.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath); err != nil {
			t.Fatal(err)
		}
		token, err := os.ReadFile(fixture.clientPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(keyPath + ".identity"); err != nil {
			t.Fatal(err)
		}
		owner, err = OpenValidatorMeasurementClientKey(t.Context(), keyPath, true)
		if owner != nil {
			_ = owner.Close()
		}
		after, readErr := os.ReadFile(fixture.clientPath)
		_, markerErr := os.Stat(keyPath + ".identity")
		if err == nil || owner != nil || readErr != nil || !bytes.Equal(token, after) || !errors.Is(markerErr, os.ErrNotExist) {
			t.Fatal("measurement missing key marker blessed retained versioned client custody", kind, err)
		}
	}
}

func TestMeasurementRegistrationAndCallbacksRejectReplacedRoot(t *testing.T) {
	for _, action := range []string{"register", "refresh", "logout"} {
		fixture, keyPath := newMeasurementRegistrationFixture(t)
		owner := openMeasurementFixtureOwner(t, keyPath, true)
		dir := filepath.Dir(keyPath)
		moved := dir + "-retained"
		if err := os.Rename(dir, moved); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(moved) })
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		before := registrationTestToken(t, testClientId, "replacement")
		if err := WriteToken(fixture.clientPath, before); err != nil {
			t.Fatal(err)
		}
		var err error
		switch action {
		case "register":
			_, _, err = owner.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath)
		case "refresh":
			err = owner.PersistClientJwt(registrationTestToken(t, testClientId, "refresh"))
		case "logout":
			err = owner.RejectClientJwt()
		}
		after, readErr := ReadToken(fixture.clientPath)
		_, rejectedErr := os.Stat(fixture.clientPath + ".rejected")
		if err == nil || fixture.postCount() != 0 || readErr != nil || after != before || !errors.Is(rejectedErr, os.ErrNotExist) {
			t.Fatal("measurement operation escaped its original physical key root", action, err)
		}
	}
}

func TestMeasurementRegistrationSeparateBootstrapRetainsPhysicalOwner(t *testing.T) {
	fixture, keyPath := newMeasurementRegistrationFixture(t)
	owner := openMeasurementFixtureOwner(t, keyPath, true)
	bootstrapDir := t.TempDir()
	if err := os.Chmod(bootstrapDir, 0700); err != nil {
		t.Fatal(err)
	}
	fixture.networkPath = filepath.Join(bootstrapDir, "jwt")
	if err := WriteToken(fixture.networkPath, registrationTestToken(t, "", "original")); err != nil {
		t.Fatal(err)
	}
	original := retainMeasurementFixtureOperation(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	fixture.onCommit = cancel
	_, _, _ = owner.LoadOrRegisterClientJwt(ctx, fixture.api, fixture.networkPath)
	if fixture.postCount() != 1 {
		t.Fatal("measurement separate-bootstrap fixture did not reach retained replay")
	}
	moved := bootstrapDir + "-retained"
	if err := os.Rename(bootstrapDir, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if err := os.Mkdir(bootstrapDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := WriteToken(fixture.networkPath, registrationTestToken(t, "", "equal-principal-replacement")); err != nil {
		t.Fatal(err)
	}
	_, _, err := owner.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath)
	after, readErr := os.ReadFile(fixture.clientPath + ".registration")
	if err == nil || fixture.postCount() != 1 || readErr != nil || !bytes.Equal(original, after) {
		t.Fatal("measurement replay rebound its separate bootstrap directory", err)
	}
}

func TestMeasurementRegistrationRevocationRemainsSticky(t *testing.T) {
	fixture, keyPath := newMeasurementRegistrationFixture(t)
	owner := openMeasurementFixtureOwner(t, keyPath, true)
	if _, _, err := owner.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath); err != nil {
		t.Fatal(err)
	}
	if err := owner.RejectClientJwt(); err != nil {
		t.Fatal(err)
	}
	if err := WriteToken(fixture.networkPath, registrationTestToken(t, "", "new-login")); err != nil {
		t.Fatal(err)
	}
	// A stale refresh or externally restored token cannot erase the tombstone.
	if err := owner.PersistClientJwt(registrationTestToken(t, testClientId, "late")); err == nil {
		t.Fatal("measurement stale refresh resurrected a revoked credential")
	}
	if err := WriteToken(fixture.clientPath, registrationTestToken(t, testClientId, "restored")); err != nil {
		t.Fatal(err)
	}
	_, _, err := owner.LoadOrRegisterClientJwt(t.Context(), fixture.api, fixture.networkPath)
	var refused *RegistrationRefusedError
	if !errors.As(err, &refused) || refused.Code != "client_revoked" || fixture.postCount() != 0 {
		t.Fatal("measurement login rotation cleared original revocation", err)
	}
}
