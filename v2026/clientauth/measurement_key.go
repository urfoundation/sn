// Measurement-only validators retain their pre-existing key and client. This
// owner cannot generate a key or authorize a new server client allocation.
package clientauth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// The runner closes this owner only after its API callbacks and all measurement
// workers join. Authentication attempts are sequential; callbacks may persist
// or reject only while the runner retains the owner.
type ValidatorMeasurementClientKeyOwner struct {
	store         *registrationStore
	seed          []byte
	bootstrap     *registrationStore
	clientPath    string
	bootstrapPath string
}

// Legacy adoption is an explicit assertion that the retained seed is original.
// JWT claims alone do not prove its connection to that key. It grants no create
// permission, and cannot repair missing versioned key identity custody.
func OpenValidatorMeasurementClientKey(ctx context.Context, keyPath string, adoptLegacyKey bool) (_ *ValidatorMeasurementClientKeyOwner, returnErr error) {
	if ctx == nil || filepath.Base(keyPath) != ".validator.key" {
		return nil, errors.New("measurement key requires its context and canonical key name")
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
	seed, err := store.read(".validator.key")
	if errors.Is(err, os.ErrNotExist) {
		return nil, &RegistrationRefusedError{Code: "measurement_key_requires_explicit_recovery"}
	}
	if err != nil {
		return nil, err
	}
	if len(seed) != ed25519.SeedSize || bytes.Equal(seed, make([]byte, ed25519.SeedSize)) {
		return nil, errors.New("measurement client key must be an existing nonzero 32-byte seed")
	}
	public := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	expected := []byte("urnetwork-validator-measurement-client-key-v1\n0x" + hex.EncodeToString(public) + "\n")
	marker, err := store.read(".validator.key.identity")
	if errors.Is(err, os.ErrNotExist) {
		if !adoptLegacyKey {
			return nil, &RegistrationRefusedError{Code: "measurement_key_requires_legacy_adoption"}
		}
		names, err := store.names(4096)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if strings.HasPrefix(name, ".validator") && name != ".validator.key" && name != ".validator.key.registration.lock" && name != ".validator.jwt" {
				return nil, &RegistrationRefusedError{Code: "measurement_key_identity_missing_with_retained_history"}
			}
		}
		raw, err := store.read(".validator.jwt")
		if err != nil {
			return nil, &RegistrationRefusedError{Code: "measurement_legacy_credential_requires_recovery"}
		}
		identity, err := registrationIdentity(strings.TrimSpace(string(raw)))
		if err != nil || !validRegistrationIdentityId(identity.clientId) || !validRegistrationIdentityId(identity.deviceId) || !validRegistrationIdentityId(identity.NetworkId) || !validRegistrationIdentityId(identity.UserId) {
			return nil, errors.New("measurement legacy adoption requires a retained client identity")
		}
		if err := store.write(".validator.key.identity", expected); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if !bytes.Equal(marker, expected) {
		return nil, errors.New("measurement client key differs from its retained public identity")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	self.seed = seed
	return self, nil
}

// Only original operation replay or original client refresh is possible. The
// global login directory is lazily retained when that operation needs it;
// an existing client refresh does not need the powerful bootstrap credential.
func (self *ValidatorMeasurementClientKeyOwner) LoadOrRegisterClientJwt(ctx context.Context, api *sdk.Api, networkPath string) (string, connect.Id, error) {
	if self == nil || self.store == nil || len(self.seed) != ed25519.SeedSize || api == nil || filepath.Base(networkPath) != "jwt" {
		return "", connect.Id{}, errors.New("measurement registration lacks retained custody")
	}
	if err := self.store.check(); err != nil {
		return "", connect.Id{}, err
	}
	if _, err := self.store.read(".validator.jwt.rejected"); err == nil {
		return "", connect.Id{}, &RegistrationRefusedError{Code: "client_revoked"}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", connect.Id{}, err
	}
	endpoint, err := api.NetworkClientRegistrationEndpoint()
	if err != nil {
		return "", connect.Id{}, err
	}
	public := ed25519.NewKeyFromSeed(self.seed).Public().(ed25519.PublicKey)
	scope := RegistrationScope{Endpoint: endpoint, ClientKey: "0x" + hex.EncodeToString(public), ClientRole: "validator-measurement-v1", ClientSlot: "direct"}
	bootstrapOwner := func() (*registrationStore, error) {
		if self.bootstrap == nil {
			store, err := openRegistrationStore(networkPath)
			if err != nil {
				return nil, err
			}
			self.bootstrap = store
			self.bootstrapPath = networkPath
		}
		if self.bootstrapPath != networkPath {
			return nil, errors.New("measurement bootstrap path differs from its retained owner")
		}
		return self.bootstrap, self.bootstrap.check()
	}
	token, id, err := loadOrRegisterClientJwtWithCustody(ctx, api, networkPath, self.clientPath, "validator measurement", scope, false, registrationHooks{}, self.store, bootstrapOwner)
	if err != nil {
		return "", connect.Id{}, err
	}
	// Completed primary authentication needs only the client credential. Keep
	// the original key owner, but do not monopolize a shared login directory
	// while unrelated measurement states may need their own retained replay.
	if self.bootstrap != nil {
		err := self.bootstrap.close()
		self.bootstrap, self.bootstrapPath = nil, ""
		if err != nil {
			return "", connect.Id{}, err
		}
	}
	return token, id, nil
}

// Renewal is published only in the key owner's original directory. The caller
// must validate the original stable client/principal before invoking this method.
func (self *ValidatorMeasurementClientKeyOwner) PersistClientJwt(token string) (returnErr error) {
	if self == nil || self.store == nil || strings.TrimSpace(token) == "" {
		return errors.New("measurement refresh lacks owned custody")
	}
	store, err := openRegistrationStoreForOwner(self.clientPath, self.store)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, store.close()) }()
	if _, err := store.read(".validator.jwt.rejected"); err == nil {
		return &RegistrationRefusedError{Code: "client_revoked"}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return store.write(".validator.jwt", []byte(strings.TrimSpace(token)))
}

// A confirmed rejection remains sticky across login rotation and process restarts.
func (self *ValidatorMeasurementClientKeyOwner) RejectClientJwt() (returnErr error) {
	if self == nil || self.store == nil {
		return errors.New("measurement rejection lacks owned custody")
	}
	store, err := openRegistrationStoreForOwner(self.clientPath, self.store)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, store.close()) }()
	return errors.Join(store.write(".validator.jwt.rejected", []byte("blocked")), store.remove(".validator.jwt"))
}

// Seed returns a copy for the sole measurement transport's immutable settings.
func (self *ValidatorMeasurementClientKeyOwner) Seed() []byte { return slices.Clone(self.seed) }

// Close belongs to the enclosing runner after every user has joined.
func (self *ValidatorMeasurementClientKeyOwner) Close() error {
	if self == nil {
		return nil
	}
	var bootstrapErr error
	if self.bootstrap != nil {
		bootstrapErr = self.bootstrap.close()
	}
	return errors.Join(bootstrapErr, self.store.close())
}
