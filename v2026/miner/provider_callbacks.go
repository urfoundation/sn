// These are the actual provide authentication and key callbacks. Required file
// effects retain their prior order and cancellation; logging is only an offer.
package miner

import (
	"context"
	"sync"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
)

// SDK refresh/logout callbacks may share a dispatch worker. They never wait on
// diagnostic delivery or join their own API/device. The first file fault stays
// available to the enclosing provider after callback ownership is released.
type providerAuthenticationCallbacks struct {
	stateLock      sync.Mutex
	firstErr       error
	diagnostics    *providerDiagnostics
	provider       uint64
	clientJwtPath  string
	networkJwtPath string
	cancel         context.CancelFunc
	custody        *clientauth.ProviderClientKeyOwner
}

// Required token persistence precedes its existing global cancellation policy.
func (self *providerAuthenticationCallbacks) JwtRefreshed(jwt string) {
	var err error
	if self.custody != nil {
		err = self.custody.PersistClientJwt(self.clientJwtPath, jwt)
	} else {
		err = clientauth.WriteToken(self.clientJwtPath, jwt)
	}
	if err != nil {
		self.retainFailure(err)
		self.cancel()
		self.diagnostics.observe(providerJwtSaveFailed, self.provider, true, err, 0, nil)
	}
}

// A rejected credential is tombstoned before requesting shutdown. A failed
// tombstone is retained, without blocking cancellation on an output write.
func (self *providerAuthenticationCallbacks) AuthLogout() {
	var err error
	if self.custody != nil {
		err = self.custody.RejectClientJwt(self.clientJwtPath)
	} else {
		err = clientauth.MarkRejected(self.clientJwtPath, self.networkJwtPath)
	}
	if err != nil {
		self.retainFailure(err)
	}
	self.cancel()
	if err != nil {
		self.diagnostics.observe(providerRejectionSaveFailed, self.provider, true, err, 0, nil)
	}
	self.diagnostics.observe(providerAuthenticationRejected, self.provider, true, nil, 0, nil)
}

// Retain one original error without formatting or growing a callback history.
func (self *providerAuthenticationCallbacks) retainFailure(err error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.firstErr == nil {
		self.firstErr = err
	}
}

// The owner reads this after the API callback lifecycle has joined.
func (self *providerAuthenticationCallbacks) failure() error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.firstErr
}

// These key saves keep their existing best-effort startup semantics. Each real
// failure remains visible as its own closed event; output never gates serving.
func persistProviderKeyMaterial(output *providerDiagnostics, provider uint64, keyMaterial *sdk.DeviceLocalKeyMaterial) {
	if seed := keyMaterial.GetClientKeySeed(); len(seed) > 0 {
		if err := writeProviderClientKeySeed(seed); err != nil {
			output.observe(providerClientKeySaveFailed, provider, true, err, 0, nil)
		}
	}
	persistProviderAuxiliaryKeyMaterial(output, provider, keyMaterial)
}

// Device-generated TLS/extender state cannot overwrite the already owned and
// fsynced client key that selected the durable registration operation.
func persistProviderAuxiliaryKeyMaterial(output *providerDiagnostics, provider uint64, keyMaterial *sdk.DeviceLocalKeyMaterial) {
	certPem, keyPem := keyMaterial.GetProvideTlsCertificatePem(), keyMaterial.GetProvideTlsPrivateKeyPem()
	if len(certPem) > 0 && len(keyPem) > 0 {
		if err := writeProviderTlsCertAndKey(certPem, keyPem); err != nil {
			output.observe(providerTlsKeySaveFailed, provider, true, err, 0, nil)
		}
	}
	if seed := keyMaterial.GetExtenderKeySeed(); len(seed) > 0 {
		if err := writeProviderExtenderKeySeed(seed); err != nil {
			output.observe(providerExtenderKeySaveFailed, provider, true, err, 0, nil)
		}
	}
}

// Derive only the availability fact; neither public identity nor private seed
// enters a callback log record. This remains independent of persistence success.
func observeProviderExtenderIdentity(output *providerDiagnostics, provider uint64, seed []byte) {
	if len(seed) == 0 {
		return
	}
	if _, err := connect.ExtenderPublicKeyFromSeed(seed); err != nil {
		output.observe(providerExtenderIdentityInvalid, provider, true, err, 0, nil)
		return
	}
	output.observe(providerIdentityReady, provider, true, nil, 0, nil)
}

// Only changed scalar facts are offered. SDK error/address strings are never
// formatted or retained, and exporter calls occur after the state lock releases.
type providerExtenderStatusListener struct {
	stateLock   sync.Mutex
	observed    bool
	last        providerExtenderObservation
	diagnostics *providerDiagnostics
	provider    uint64
}

// The provider borrows its enclosing run's explicitly supplied output budget.
func newProviderExtenderStatusListener(output *providerDiagnostics, provider uint64) *providerExtenderStatusListener {
	return &providerExtenderStatusListener{diagnostics: output, provider: provider}
}

// A nil observation is unknown, separate from a known disabled extender.
func (self *providerExtenderStatusListener) ExtenderProvideStatusChanged(status *sdk.ExtenderProvideStatus) {
	observation := providerExtenderObservation{}
	if status != nil {
		observation = providerExtenderObservation{Known: true, Enabled: status.Enabled, Listening: status.Listening, ListenFailed: status.ListenError != "", ActivatedV4: status.ActivatedV4, ActivatedV6: status.ActivatedV6, ActivationFailed: status.LastActivationError != "", Revoked: status.RevokedTime != 0}
	}
	changed := func() bool {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.observed && self.last == observation {
			return false
		}
		self.observed, self.last = true, observation
		return true
	}()
	if changed {
		self.diagnostics.observe(providerExtenderObserved, self.provider, true, nil, 0, &observation)
	}
}
