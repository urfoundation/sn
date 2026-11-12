// These are the actual provide authentication and key callbacks. Required file
// effects retain their prior order and cancellation; logging is only an offer.
package miner

import (
	"context"
	"os"
	"sync"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"

	"github.com/urfoundation/sn/clientauth"
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
// The one reason it adds is a closed vocabulary (extender_standing.go), with
// the guidance that says the fix.
type providerExtenderStatusListener struct {
	stateLock   sync.Mutex
	observed    bool
	last        providerExtenderObservation
	diagnostics *providerDiagnostics
	provider    uint64
	// where the status file reads this slot's standing, nil for none
	standings *minerExtenderStandings
	// the command that lets this miner bind tcp 443, empty for none
	bindFix string
	// the run's port mapping, which the extender slot's listener alone feeds
	// (connect/EXTENDER.md S10); nil on every other slot
	portMapping *minerPortMappingHolder
}

// The provider borrows its enclosing run's explicitly supplied output budget.
func newProviderExtenderStatusListener(output *providerDiagnostics, provider uint64) *providerExtenderStatusListener {
	return &providerExtenderStatusListener{diagnostics: output, provider: provider}
}

// withStanding also keeps the slot's standing for the status file, and has
// the log's guidance name `bindFix` as the fix for a refused bind.
func (self *providerExtenderStatusListener) withStanding(standings *minerExtenderStandings, bindFix string) *providerExtenderStatusListener {
	self.standings, self.bindFix = standings, bindFix
	return self
}

// A nil observation is unknown, separate from a known disabled extender.
func (self *providerExtenderStatusListener) ExtenderProvideStatusChanged(status *sdk.ExtenderProvideStatus) {
	observation := providerExtenderObservation{}
	standing := minerExtenderStandingOf(status)
	if status != nil {
		observation = providerExtenderObservation{Known: true, Enabled: status.Enabled, Listening: status.Listening, ListenFailed: status.ListenError != "", ActivatedV4: status.ActivatedV4, ActivatedV6: status.ActivatedV6, ActivationFailed: status.LastActivationError != "", Revoked: status.RevokedTime != 0, Reason: standing.Reason, PortMapping: status.PortMapping}
	}
	self.standings.set(self.provider, standing)
	// the status file keeps the router port mapping's state, method and ports,
	// a closed vocabulary and numbers, never the router's words
	self.portMapping.observe(status)
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
		self.diagnostics.observeGuided(providerExtenderObserved, self.provider, true, nil, 0, &observation, "", minerExtenderGuidance(observation.Reason, self.bindFix, os.Geteuid()))
	}
}
