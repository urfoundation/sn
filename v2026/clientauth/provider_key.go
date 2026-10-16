// Provider key custody precedes client allocation and spans all proxy workers.
// A missing retained key is recovery work, never a request for a new identity.
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
	"slices"
	"strings"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// The caller owns Close after every device and authentication worker has joined.
// Seed returns a copy; the retained key is never rewritten by a device callback.
type ProviderClientKeyOwner struct {
	store *registrationStore
	seed  []byte
}

// These are separate operator decisions. Legacy adoption asserts that the
// retained seed is the recovered original; JWT claims cannot prove that link.
// Adoption permits no new client allocation and cannot repair versioned history.
type ProviderClientKeyOptions struct {
	AllowCreate    bool
	AdoptLegacyKey bool
}

// This observer can stop only after actual seed publication and directory sync.
type providerClientKeyHooks struct{ afterSeed func() error }

// Explicit creation is meaningful only without any retained provider history.
// Complete loss of every custody file cannot be distinguished from a new install.
func OpenProviderClientKey(ctx context.Context, keyPath string, options ProviderClientKeyOptions) (_ *ProviderClientKeyOwner, returnErr error) {
	return openProviderClientKey(ctx, keyPath, options, providerClientKeyHooks{})
}

// The internal observer does not replace a key read, random seed or fsync.
func openProviderClientKey(ctx context.Context, keyPath string, options ProviderClientKeyOptions, hooks providerClientKeyHooks) (_ *ProviderClientKeyOwner, returnErr error) {
	if ctx == nil || filepath.Base(keyPath) != ".provider.key" {
		return nil, errors.New("provider key requires its context and canonical key name")
	}
	if options.AllowCreate && options.AdoptLegacyKey {
		return nil, errors.New("provider legacy key adoption cannot authorize new creation")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store, err := openRegistrationStore(keyPath)
	if err != nil {
		return nil, err
	}
	self := &ProviderClientKeyOwner{store: store}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, self.Close())
		}
	}()
	seed, err := store.read(".provider.key")
	if errors.Is(err, os.ErrNotExist) {
		if !options.AllowCreate {
			return nil, &RegistrationRefusedError{Code: "provider_key_requires_explicit_recovery"}
		}
		names, err := store.names(4096)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if strings.HasPrefix(name, ".provider") && name != ".provider.key.registration.lock" {
				return nil, &RegistrationRefusedError{Code: "provider_key_missing_with_retained_history"}
			}
		}
		seed = make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := store.write(".provider.key", seed); err != nil {
			return nil, err
		}
		if hooks.afterSeed != nil {
			if err := hooks.afterSeed(); err != nil {
				return nil, err
			}
		}
	} else if err != nil {
		return nil, err
	}
	if len(seed) != ed25519.SeedSize || bytes.Equal(seed, make([]byte, ed25519.SeedSize)) {
		return nil, errors.New("provider client key must be an existing nonzero 32-byte seed")
	}
	public := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	expected := []byte("urnetwork-provider-client-key-v1\n0x" + hex.EncodeToString(public) + "\n")
	marker, err := store.read(".provider.key.identity")
	if errors.Is(err, os.ErrNotExist) {
		if err := admitProviderKeyMarker(store, options.AdoptLegacyKey); err != nil {
			return nil, err
		}
		if err := store.write(".provider.key.identity", expected); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if !bytes.Equal(marker, expected) {
		return nil, errors.New("provider client key differs from its retained public identity")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	self.seed = seed
	return self, nil
}

// A bare retained seed can finish the key-before-marker crash window. Existing
// provider history cannot: without a marker its original key is unproven. A
// separate first-upgrade assertion can adopt only legacy JWTs and legacy side
// files, never a retained versioned operation or evidence of an earlier upgrade.
func admitProviderKeyMarker(store *registrationStore, adoptLegacy bool) error {
	names, err := store.names(4096)
	if err != nil {
		return err
	}
	var history, credentials bool
	for _, name := range names {
		if !strings.HasPrefix(name, ".provider") || name == ".provider.key" || name == ".provider.key.registration.lock" {
			continue
		}
		history = true
		if !adoptLegacy {
			return &RegistrationRefusedError{Code: "provider_key_identity_missing_with_retained_history"}
		}
		if name == ".provider.cert" || name == ".provider.extender.key" {
			continue
		}
		if !providerClientCredentialName(name) {
			return &RegistrationRefusedError{Code: "provider_key_legacy_adoption_has_nonlegacy_history"}
		}
		raw, err := store.read(name)
		if err != nil {
			return err
		}
		identity, err := registrationIdentity(strings.TrimSpace(string(raw)))
		if err != nil || !validRegistrationIdentityId(identity.clientId) || !validRegistrationIdentityId(identity.deviceId) {
			return errors.New("provider legacy key adoption requires retained client identity")
		}
		credentials = true
	}
	if history && !credentials {
		return &RegistrationRefusedError{Code: "provider_key_legacy_adoption_lacks_credential"}
	}
	return nil
}

// Only the direct credential or the established proxy credential name can be
// written through this key owner. Callers cannot select its seed or marker.
func providerClientCredentialName(name string) bool {
	if name == ".provider.jwt" {
		return true
	}
	if !strings.HasPrefix(name, ".provider-") || !strings.HasSuffix(name, ".jwt") {
		return false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(name, ".provider-"), ".jwt")
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 8 && value == hex.EncodeToString(raw)
}

// Every attempt borrows the same physical key directory. The immutable cached
// key is useful only with that original custody, never a replacement pathname.
func (self *ProviderClientKeyOwner) LoadOrRegisterClientJwt(ctx context.Context, api *sdk.Api, networkPath, clientPath, slot string, allowCreate bool) (string, connect.Id, error) {
	if self == nil || self.store == nil || len(self.seed) != ed25519.SeedSize || !providerClientCredentialName(filepath.Base(clientPath)) || api == nil {
		return "", connect.Id{}, errors.New("provider registration lacks its retained custody owner")
	}
	endpoint, err := api.NetworkClientRegistrationEndpoint()
	if err != nil {
		return "", connect.Id{}, err
	}
	public := ed25519.NewKeyFromSeed(self.seed).Public().(ed25519.PublicKey)
	scope := RegistrationScope{Endpoint: endpoint, ClientKey: "0x" + hex.EncodeToString(public), ClientRole: "provider-v1", ClientSlot: slot}
	return loadOrRegisterClientJwtInDirectory(ctx, api, networkPath, clientPath, "provider", scope, allowCreate, registrationHooks{}, self.store)
}

// A completed refresh can update only a credential in the key owner's original
// directory. The enclosing API callback still validates its original identity.
func (self *ProviderClientKeyOwner) PersistClientJwt(clientPath, token string) (returnErr error) {
	if self == nil || self.store == nil || !providerClientCredentialName(filepath.Base(clientPath)) || strings.TrimSpace(token) == "" {
		return errors.New("provider refresh lacks owned credential custody")
	}
	store, err := openRegistrationStoreForOwner(clientPath, self.store)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, store.close()) }()
	return store.write(filepath.Base(clientPath), []byte(strings.TrimSpace(token)))
}

// A confirmed rejection is sticky in original provider custody. It never reads
// a replacement directory's bootstrap or uses fresh login as creation authority.
func (self *ProviderClientKeyOwner) RejectClientJwt(clientPath string) (returnErr error) {
	if self == nil || self.store == nil || !providerClientCredentialName(filepath.Base(clientPath)) {
		return errors.New("provider rejection lacks owned credential custody")
	}
	store, err := openRegistrationStoreForOwner(clientPath, self.store)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, store.close()) }()
	return errors.Join(store.remove(filepath.Base(clientPath)), store.write(filepath.Base(rejectionPath(clientPath)), []byte("blocked")))
}

// Independent device settings receive their own immutable seed copy.
func (self *ProviderClientKeyOwner) Seed() []byte { return slices.Clone(self.seed) }

// Closing belongs to the enclosing service after every user has joined.
func (self *ProviderClientKeyOwner) Close() error {
	if self == nil {
		return nil
	}
	return self.store.close()
}
