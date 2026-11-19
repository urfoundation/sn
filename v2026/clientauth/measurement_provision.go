// First provisioning of a pristine validator measurement directory, the
// separately approved workflow behind validator --auto-register. It may create
// one measurement key and one direct client; it never recovers, adopts or
// replaces an identity.
//
// The provisioning marker, the identity marker and then the key are each
// published durably, key last, so a durable key proves both markers. A crash
// before the key leaves a keyless remnant that every path refuses, and nothing
// was sent. A crash after it resumes: the key with both matching markers, and
// no registration, credential or other measurement file, may still make its
// one creation. Once a registration record exists the ordinary replay and
// refresh rules apply.
//
// The authority is spent durably. Whichever path completes the registration,
// provisioning or the plain run's replay or refresh, renames the provisioning
// marker to .validator.key.provisioned with the same bytes once the operation
// is retained and the client JWT persisted. The provisioned marker is
// provenance only: it authorizes nothing, and provisioning refuses any
// directory that holds it.
package clientauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

const measurementProvisioningMarker = ".validator.key.provisioning"
const measurementProvisionedMarker = ".validator.key.provisioned"

// Test observers stop only after a durable publication. They cannot replace a
// read, the random seed, an fsync or the registration.
type measurementProvisionHooks struct {
	afterKey func() error
	// After the provisioned registration completes, before the marker rename.
	afterRegistration func() error
}

// The owner holds the directory exactly as OpenValidatorMeasurementClientKey
// does, and ProvisionClientJwt may make its one direct registration. A
// directory that is neither pristine nor an interrupted provisioning is
// refused with measurement_provisioning_requires_pristine_directory and left
// unchanged.
func ProvisionValidatorMeasurementClientKey(ctx context.Context, keyPath string) (*ValidatorMeasurementClientKeyOwner, error) {
	return provisionValidatorMeasurementClientKey(ctx, keyPath, measurementProvisionHooks{})
}

func provisionValidatorMeasurementClientKey(ctx context.Context, keyPath string, hooks measurementProvisionHooks) (_ *ValidatorMeasurementClientKeyOwner, returnErr error) {
	if ctx == nil || filepath.Base(keyPath) != ".validator.key" {
		return nil, errors.New("measurement provisioning requires its context and canonical key name")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store, err := openRegistrationStore(keyPath)
	if err != nil {
		return nil, err
	}
	self := &ValidatorMeasurementClientKeyOwner{store: store, clientPath: filepath.Join(filepath.Dir(keyPath), ".validator.jwt")}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, self.Close())
		}
	}()
	names, err := store.names(4096)
	if err != nil {
		return nil, err
	}
	retained := map[string]bool{}
	for _, name := range names {
		// Opening the store creates its lock, so a refused plain open of a
		// pristine directory leaves it behind.
		if strings.HasPrefix(name, ".validator") && name != ".validator.key.registration.lock" {
			retained[name] = true
		}
	}
	var seed []byte
	if len(retained) == 0 {
		seed, err = provisionMeasurementKey(ctx, store, hooks)
	} else {
		seed, err = resumeMeasurementKey(store, retained)
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	self.seed, self.provisioning, self.hooks = seed, true, hooks
	return self, nil
}

func provisionMeasurementKey(ctx context.Context, store *registrationStore, hooks measurementProvisionHooks) ([]byte, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	if bytes.Equal(seed, make([]byte, ed25519.SeedSize)) {
		return nil, errors.New("measurement provisioning drew a zero seed")
	}
	marker := measurementKeyMarker(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	for _, file := range []struct {
		name string
		raw  []byte
	}{
		{name: measurementProvisioningMarker, raw: marker},
		{name: ".validator.key.identity", raw: marker},
		{name: ".validator.key", raw: seed},
	} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := store.write(file.name, file.raw); err != nil {
			return nil, err
		}
	}
	if hooks.afterKey != nil {
		if err := hooks.afterKey(); err != nil {
			return nil, err
		}
	}
	return seed, nil
}

// Only the exact interrupted provisioning resumes: the key and both markers
// naming it, plus at most the credential lock a refused plain run creates. A
// provisioned marker, registration, credential, refusal or any other file
// means the one creation may already have happened.
func resumeMeasurementKey(store *registrationStore, retained map[string]bool) ([]byte, error) {
	refused := &RegistrationRefusedError{Code: "measurement_provisioning_requires_pristine_directory"}
	if retained[measurementProvisionedMarker] {
		return nil, refused
	}
	for _, name := range []string{".validator.key", ".validator.key.identity", measurementProvisioningMarker} {
		if !retained[name] {
			return nil, refused
		}
	}
	for name := range retained {
		switch name {
		case ".validator.key", ".validator.key.identity", measurementProvisioningMarker, ".validator.jwt.registration.lock":
		default:
			return nil, refused
		}
	}
	seed, err := store.read(".validator.key")
	if err != nil {
		return nil, err
	}
	if len(seed) != ed25519.SeedSize || bytes.Equal(seed, make([]byte, ed25519.SeedSize)) {
		return nil, refused
	}
	expected := measurementKeyMarker(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	for _, name := range []string{".validator.key.identity", measurementProvisioningMarker} {
		marker, err := store.read(name)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(marker, expected) {
			return nil, refused
		}
	}
	return seed, nil
}

// The one direct client the provisioned key may create, published as
// .validator.jwt beside it. With a registration record already present this
// is the ordinary replay or refresh. After success the owner can create
// nothing more.
func (self *ValidatorMeasurementClientKeyOwner) ProvisionClientJwt(ctx context.Context, api *sdk.Api, networkPath string) (string, connect.Id, error) {
	if self == nil || !self.provisioning {
		return "", connect.Id{}, errors.New("measurement owner holds no provisioning authority")
	}
	token, id, err := self.loadOrRegisterClientJwt(ctx, api, networkPath, true)
	if err == nil {
		self.provisioning = false
	}
	return token, id, err
}

// Creation needs the key owner's own directory and a provisioning marker that
// names exactly the scope's direct client key. A provisioned marker, or one
// that can't be read, means the authority is spent.
func measurementProvisioningAuthorized(directory *registrationStore, scope RegistrationScope) bool {
	if directory == nil || scope.ClientRole != "validator-measurement-v1" || scope.ClientSlot != "direct" {
		return false
	}
	public, err := hex.DecodeString(strings.TrimPrefix(scope.ClientKey, "0x"))
	if err != nil || len(public) != ed25519.PublicKeySize || scope.ClientKey != "0x"+hex.EncodeToString(public) {
		return false
	}
	if _, err := directory.read(measurementProvisionedMarker); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	marker, err := directory.read(measurementProvisioningMarker)
	return err == nil && bytes.Equal(marker, measurementKeyMarker(public))
}
